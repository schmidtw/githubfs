// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package githubfs

import (
	_ "embed"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	tests := []struct {
		description   string
		opts          []Option
		ghUrl         string
		rawUrl        string
		nilHttpClient bool
		threshold     int
		inputs        []input
	}{
		{
			description: "basic test",
		}, {
			description: "different github url",
			ghUrl:       "https://example.com",
			opts:        []Option{WithGithubURL("https://example.com")},
		}, {
			description: "different http url",
			rawUrl:      "https://example.com",
			opts:        []Option{WithRawDownloadURL("https://example.com")},
		}, {
			description:   "different http client",
			nilHttpClient: true,
			opts:          []Option{WithHttpClient(nil)},
		}, {
			description: "different fetch threshold",
			threshold:   10,
			opts:        []Option{WithThresholdInKB(10)},
		}, {
			description: "specify orgs and repos.",
			opts: []Option{WithOrg("foo"),
				WithOrg("bar", true),
				WithRepo("org", "repo"),
				WithRepo("cat", "repo", "branch1", "branch2"),
			},
			inputs: []input{
				{
					org: "foo",
				}, {
					org:           "bar",
					allowArchived: true,
				}, {
					org:           "org",
					repo:          "repo",
					allowArchived: true,
				}, {
					org:           "cat",
					repo:          "repo",
					branch:        "branch1",
					allowArchived: true,
				}, {
					org:           "cat",
					repo:          "repo",
					branch:        "branch2",
					allowArchived: true,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			gfs := New(tc.opts...)
			assert.NotNil(gfs)
			if len(tc.ghUrl) != 0 {
				assert.Equal(tc.ghUrl, gfs.githubUrl)
			}
			if len(tc.rawUrl) != 0 {
				assert.Equal(tc.rawUrl, gfs.rawUrl)
			}
			if tc.nilHttpClient {
				assert.Nil(gfs.httpClient)
			} else {
				assert.NotNil(gfs.httpClient)
			}
			if tc.threshold != 0 {
				assert.Equal(tc.threshold, gfs.threshold)
			}

			require.Equal(len(tc.inputs), len(gfs.inputs))
			for i := range tc.inputs {
				assert.Equal(tc.inputs[i].org, gfs.inputs[i].org)
				assert.Equal(tc.inputs[i].repo, gfs.inputs[i].repo)
				assert.Equal(tc.inputs[i].branch, gfs.inputs[i].branch)
				assert.Equal(tc.inputs[i].allowArchived, gfs.inputs[i].allowArchived)
			}
		})
	}
}

func TestMostThings(t *testing.T) {
	tests := []struct {
		description string
		opts        []Option
		statusCode  []int
		payload     []string
		ct          []string
		expectErr   bool
		expect      []string
		unexpected  []string
	}{
		{
			description: "fetch a single repo test",
			opts:        []Option{WithRepo("org", "repo")},
			payload:     []string{singleRepoReponse},
			expect:      []string{".", "org/repo/git"},
		}, {
			description: "fetch a single repo that is archived test",
			opts:        []Option{WithRepo("org", "repo")},
			payload:     []string{singleArchivedRepoReponse},
			expect:      []string{"org/repo/git"},
		}, {
			description: "fetch a disabled repo",
			opts:        []Option{WithRepo("org", "repo")},
			payload:     []string{singleDisabledRepoReponse},
			expect:      []string{"org/repo/git"},
			expectErr:   true,
		}, {
			description: "get an invalid response during fetching a single repo",
			opts:        []Option{WithRepo("org", "repo")},
			payload:     []string{invalidJsonResponse},
			expect:      []string{"org/repo/git"},
			expectErr:   true,
		}, {
			description: "fetch a single repo with a mismatched org/repo test", // happens if you move a repo to an org
			opts:        []Option{WithRepo("org", "moved")},
			payload:     []string{singleRepoReponse},
			expect:      []string{"org/repo/git", "org/moved/git"},
			expectErr:   true,
		}, {
			description: "fetch a few of repos test",
			opts:        []Option{WithOrg("org")},
			payload:     []string{twoReposResponse},
			expect:      []string{"org/.github/git", "org/.go-template/git"},
		}, {
			description: "fetch a few of repos spread across requests test",
			opts:        []Option{WithOrg("org")},
			payload:     []string{twoReposOneAtATimeResponse001, twoReposOneAtATimeResponse002},
			expect:      []string{"org/.github/git", "org/.go-template/git"},
		}, {
			description: "fetch a disabled and archived repo.",
			opts:        []Option{WithOrg("org")},
			payload:     []string{twoReposOneDisabledResponse},
			unexpected:  []string{"org/.github/git", "org/.go-template/git"},
		}, {
			description: "get an invalid response during fetching a single repo",
			opts:        []Option{WithOrg("org")},
			payload:     []string{invalidJsonResponse},
			expect:      []string{"org/repo/git", "org/moved/git"},
			expectErr:   true,
		}, {
			description: "fetch a repo with releases",
			opts:        []Option{WithRepo("org", "repo")},
			payload:     []string{singleRepoWithReleasesReponse, releaseResponse},
			expect:      []string{"org/repo/git", "org/repo/releases/v0.6.7"},
			unexpected:  []string{"org/repo/releases/v0.6.8"},
		}, {
			description: "fetch a repo with releases and it is invalid json",
			opts:        []Option{WithRepo("org", "repo")},
			payload:     []string{singleRepoWithReleasesReponse, invalidJsonResponse},
			expect:      []string{"org/repo/releases/v0.6.7"},
			expectErr:   true,
		}, {
			description: "fetch a repo a file at a time.",
			opts:        []Option{WithRepo("org", "repo"), WithThresholdInKB(0)},
			payload:     []string{singleRepoReponse, baseDirectoryResponse, readmeResponse},
			expect:      []string{"org/repo/git/main", "org/repo/git/main/README.md"},
		}, {
			description: "fetch a repo a file at a time with invalid json.",
			opts:        []Option{WithRepo("org", "repo"), WithThresholdInKB(0)},
			payload:     []string{singleRepoReponse, invalidJsonResponse},
			expect:      []string{"org/repo/git/main/README.md"},
			expectErr:   true,
		}, {
			description: "fetch a repo a file at a time with invalid file mode.",
			opts:        []Option{WithRepo("org", "repo"), WithThresholdInKB(0)},
			payload:     []string{singleRepoReponse, baseDirectoryResponseUnownFileMode},
			expect:      []string{"org/repo/git/main/README.md"},
			expectErr:   true,
		}, {
			description: "fetch a repo all at once.",
			opts:        []Option{WithRepo("org", "repo")},
			payload:     []string{singleRepoReponse, entireRepoResponse, fullRepoTarball},
			expect:      []string{"org/repo/git/main", "org/repo/git/main/a"},
		}, {
			description: "fetch a repo all at once, but there was a json error.",
			opts:        []Option{WithRepo("org", "repo")},
			payload:     []string{singleRepoReponse, invalidJsonResponse},
			expect:      []string{"org/repo/git/main", "org/repo/git/main/a"},
			expectErr:   true,
		}, {
			description: "fetch a repo all at once, but there was an http error.",
			opts:        []Option{WithRepo("org", "repo")},
			statusCode:  []int{0, 0, 500},
			payload:     []string{singleRepoReponse, entireRepoResponse, fullRepoTarball},
			expect:      []string{"org/repo/git/main", "org/repo/git/main/a"},
			expectErr:   true,
		}, {
			description: "fetch a repo all at once, but there was an invalid url.",
			opts:        []Option{WithRepo("org", "repo")},
			payload:     []string{singleRepoReponse, entireRepoResponseInvalid},
			expect:      []string{"org/repo/git/main", "org/repo/git/main/a"},
			expectErr:   true,
		}, {
			description: "fetch a repo all at once with a gzipped tarball.",
			opts:        []Option{WithRepo("org", "repo")},
			ct:          []string{"", "", "application/x-gzip"},
			payload:     []string{singleRepoReponse, entireRepoResponse, fullRepoGZTarball},
			expect:      []string{"org/repo/git/main", "org/repo/git/main/a"},
		}, {
			description: "fetch a repo all at once with a gzipped tarball, that is not correct.",
			opts:        []Option{WithRepo("org", "repo")},
			ct:          []string{"", "", "application/x-gzip"},
			payload:     []string{singleRepoReponse, entireRepoResponse, invalidJsonResponse},
			expect:      []string{"org/repo/git/main", "org/repo/git/main/a"},
			expectErr:   true,
		}, {
			description: "fetch a repo all at once but fail due to invalid content type.",
			opts:        []Option{WithRepo("org", "repo")},
			ct:          []string{"", "", "application/invalid"},
			payload:     []string{singleRepoReponse, entireRepoResponse, invalidJsonResponse},
			expect:      []string{"org/repo/git/main", "org/repo/git/main/a"},
			expectErr:   true,
		}, {
			description: "pass in an invalid path and make sure it's caught.",
			opts:        []Option{WithRepo("org", "repo")},
			expect:      []string{"./org/repo/git/main"},
			expectErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			// Figure out the address before we start so we can replace other URLs
			// in the content.
			i := 0
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			url := "http://" + server.Listener.Addr().String()

			server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var statusSent bool
				if len(tc.ct) > 0 {
					if i < len(tc.ct) && len(tc.ct[i]) > 0 {
						w.Header().Add("Content-Type", tc.ct[i])
					}
				}

				if len(tc.statusCode) > 0 {
					if i < len(tc.statusCode) {
						if tc.statusCode[i] != 0 {
							w.WriteHeader(tc.statusCode[i])
							statusSent = true
						}
					} else {
						statusSent = true
						w.WriteHeader(500)
					}
				}
				if i < len(tc.payload) {
					payload := strings.ReplaceAll(tc.payload[i], "OVERWRITEURL", url)
					_, _ = fmt.Fprint(w, payload)
				} else {
					if !statusSent {
						w.WriteHeader(500)
					}
				}
				i++
			})

			server.Start()

			tc.opts = append(tc.opts, WithGithubURL(server.URL), WithRawDownloadURL(server.URL))
			gfs := New(tc.opts...)
			require.NotNil(gfs)

			for _, path := range tc.expect {
				f, err := gfs.Open(path)
				if tc.expectErr {
					assert.Error(err)
				} else {
					assert.NoError(err)
					require.NotNil(f)
					assert.NoError(f.Close())
				}
			}

			for _, path := range tc.unexpected {
				f, err := gfs.Open(path)
				assert.Error(err)
				assert.Nil(f)
			}

		})
	}
}

var singleRepoReponse = `{
  "data": {
    "repository": {
      "diskUsage": 18,
      "isArchived": false,
      "isDisabled": false,
      "nameWithOwner": "org/repo",
      "defaultBranchRef": {
        "name": "main"
      },
      "releases": {
        "totalCount": 0
      }
    }
  }
}`

var singleRepoWithReleasesReponse = `{
  "data": {
    "repository": {
      "diskUsage": 18,
      "isArchived": false,
      "isDisabled": false,
      "nameWithOwner": "org/repo",
      "defaultBranchRef": {
        "name": "main"
      },
      "releases": {
        "totalCount": 2
      }
    }
  }
}`

var singleArchivedRepoReponse = `{
  "data": {
    "repository": {
      "diskUsage": 18,
      "isArchived": true,
      "isDisabled": false,
      "nameWithOwner": "org/repo",
      "defaultBranchRef": {
        "name": "main"
      },
      "releases": {
        "totalCount": 0
      }
    }
  }
}`

var singleDisabledRepoReponse = `{
  "data": {
    "repository": {
      "diskUsage": 18,
      "isArchived": false,
      "isDisabled": true,
      "nameWithOwner": "org/repo",
      "defaultBranchRef": {
        "name": "main"
      },
      "releases": {
        "totalCount": 0
      }
    }
  }
}`

var invalidJsonResponse = `{`

var twoReposResponse = `{
  "data": {
    "repositoryOwner": {
      "repositories": {
        "edges": [
          {
            "node": {
              "name": ".github",
              "diskUsage": 77,
              "isArchived": false,
              "isDisabled": false,
              "nameWithOwner": "org/.github",
              "defaultBranchRef": {
                "name": "main"
              },
              "releases": {
                "totalCount": 0
              }
            }
          },
          {
            "node": {
              "name": ".go-template",
              "diskUsage": 43,
              "isArchived": false,
              "isDisabled": false,
              "nameWithOwner": "org/.go-template",
              "defaultBranchRef": {
                "name": "main"
              },
              "releases": {
                "totalCount": 0
              }
            }
          }
        ],
        "pageInfo": {
          "endCursor": "Y3Vyc29yOnYyOpKsLmdvLXRlbXBsYXRlzgu16xQ=",
          "hasNextPage": false
        }
      }
    }
  }
}`

var twoReposOneAtATimeResponse001 = `{
  "data": {
    "repositoryOwner": {
      "repositories": {
        "edges": [
          {
            "node": {
              "name": ".github",
              "diskUsage": 77,
              "isArchived": false,
              "isDisabled": false,
              "nameWithOwner": "org/.github",
              "defaultBranchRef": {
                "name": "main"
              },
              "releases": {
                "totalCount": 0
              }
            }
          }
        ],
        "pageInfo": {
          "endCursor": "Y3Vyc29yOnYyOpKsLmdvLXRlbXBsYXRlzgu16xQ=",
          "hasNextPage": true
        }
      }
    }
  }
}`

var twoReposOneAtATimeResponse002 = `{
  "data": {
    "repositoryOwner": {
      "repositories": {
        "edges": [
          {
            "node": {
              "name": ".go-template",
              "diskUsage": 43,
              "isArchived": false,
              "isDisabled": false,
              "nameWithOwner": "org/.go-template",
              "defaultBranchRef": {
                "name": "main"
              },
              "releases": {
                "totalCount": 0
              }
            }
          }
        ],
        "pageInfo": {
          "endCursor": "Y3Vyc29yOnYyOpKsLmdvLXRlbXBsYXRlzgu16xQ=",
          "hasNextPage": false
        }
      }
    }
  }
}`

var twoReposOneDisabledResponse = `{
  "data": {
    "repositoryOwner": {
      "repositories": {
        "edges": [
          {
            "node": {
              "name": ".github",
              "diskUsage": 77,
              "isArchived": false,
              "isDisabled": true,
              "nameWithOwner": "org/.github",
              "defaultBranchRef": {
                "name": "main"
              },
              "releases": {
                "totalCount": 0
              }
            }
          },
          {
            "node": {
              "name": ".go-template",
              "diskUsage": 43,
              "isArchived": true,
              "isDisabled": false,
              "nameWithOwner": "org/.go-template",
              "defaultBranchRef": {
                "name": "main"
              },
              "releases": {
                "totalCount": 0
              }
            }
          }
        ],
        "pageInfo": {
          "endCursor": "Y3Vyc29yOnYyOpKsLmdvLXRlbXBsYXRlzgu16xQ=",
          "hasNextPage": false
        }
      }
    }
  }
}`

var releaseResponse = `{
  "data": {
    "repository": {
      "releases": {
        "edges": [
          {
            "node": {
              "tag": {
                "name": "v0.6.8"
              },
              "isPrerelease": true,
              "isDraft": false,
              "createdAt": "2022-09-06T20:21:35Z",
              "description": "- JWT Migration [250](https://github.com/xmidt-org/talaria/issues/250)\n  - updated to use clortho Resolver\n  - updated to use clortho metrics & logging\n- Update Config\n  - Use [uber/zap](https://github.com/uber-go/zap) for clortho logging\n  - Use [xmidt-org/sallust](https://github.com/xmidt-org/sallust) for the zap config unmarshalling \n  - Update auth config for clortho\n",
              "releaseAssets": {
                "edges": [
                  {
                    "node": {
                      "downloadUrl": "https://github.com/xmidt-org/talaria/releases/download/v0.6.8/sha256sum.txt",
                      "name": "sha256sum.txt",
                      "size": 171
                    }
                  },
                  {
                    "node": {
                      "downloadUrl": "https://github.com/xmidt-org/talaria/releases/download/v0.6.8/talaria-0.6.8.tar.gz",
                      "name": "talaria-0.6.8.tar.gz",
                      "size": 136511
                    }
                  },
                  {
                    "node": {
                      "downloadUrl": "https://github.com/xmidt-org/talaria/releases/download/v0.6.8/talaria-0.6.8.zip",
                      "name": "talaria-0.6.8.zip",
                      "size": 165379
                    }
                  }
                ]
              }
            }
          },
          {
            "node": {
              "tag": {
                "name": "v0.6.7"
              },
              "isPrerelease": false,
              "isDraft": false,
              "createdAt": "2022-08-26T22:53:33Z",
              "description": "- Dependency update, note vulnerabilities\n    - github.com/hashicorp/consul/api v1.13.1 // indirect\n        Wasn't able to find much info about this one besides the following dep vulns\n        - golang.org/x/net\n            - https://nvd.nist.gov/vuln/detail/CVE-2021-33194\n            - https://nvd.nist.gov/vuln/detail/CVE-2021-31525\n            - https://nvd.nist.gov/vuln/detail/CVE-2021-44716\n    - Introduces new vuln https://www.mend.io/vulnerability-database/CVE-2022-29526\n- QOS Ack implementation [#228](https://github.com/xmidt-org/talaria/issues/228) [#236](https://github.com/xmidt-org/talaria/pull/236)\n",
              "releaseAssets": {
                "edges": [
                  {
                    "node": {
                      "downloadUrl": "https://github.com/xmidt-org/talaria/releases/download/v0.6.7/sha256sum.txt",
                      "name": "sha256sum.txt",
                      "size": 171
                    }
                  },
                  {
                    "node": {
                      "downloadUrl": "https://github.com/xmidt-org/talaria/releases/download/v0.6.7/talaria-0.6.7.tar.gz",
                      "name": "talaria-0.6.7.tar.gz",
                      "size": 133616
                    }
                  },
                  {
                    "node": {
                      "downloadUrl": "https://github.com/xmidt-org/talaria/releases/download/v0.6.7/talaria-0.6.7.zip",
                      "name": "talaria-0.6.7.zip",
                      "size": 162138
                    }
                  }
                ]
              }
            }
          }
        ]
      }
    }
  }
}`

var baseDirectoryResponse = `{
  "data": {
    "repository": {
      "object": {
        "entries": [
          {
            "name": ".github",
            "size": 0,
            "mode": 16384
          },
          {
            "name": ".gitignore",
            "size": 23,
            "mode": 33188
          },
          {
            "name": "README.md",
            "size": 47,
            "mode": 33188
          },
          {
            "name": "executable",
            "size": 529,
            "mode": 33261
          }
        ]
      }
    }
  }
}`

var baseDirectoryResponseUnownFileMode = `{
  "data": {
    "repository": {
      "object": {
        "entries": [
          {
            "name": ".github",
            "size": 0,
            "mode": 16384
          },
          {
            "name": ".gitignore",
            "size": 23,
            "mode": 33188
          },
          {
            "name": "README.md",
            "size": 47,
            "mode": 33188
          },
          {
            "name": "unknown",
            "size": 529,
            "mode": 99999
          }
        ]
      }
    }
  }
}`

var readmeResponse = `Readme.md contents`

var entireRepoResponse = `{
  "data": {
    "repository": {
      "ref": {
        "target": {
          "tarballUrl": "OVERWRITEURL"
        }
      }
    }
  }
}`

var entireRepoResponseInvalid = `{
  "data": {
    "repository": {
      "ref": {
        "target": {
          "tarballUrl": "unsupported"
        }
      }
    }
  }
}`

//go:embed tarballs/simple.tar
var fullRepoTarball string

//go:embed tarballs/simple.tar.gz
var fullRepoGZTarball string
