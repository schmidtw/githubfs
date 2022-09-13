// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

// Package githubfs is a handy filesystem approach to interacting with assets
// that github provides.
//
// Githubfs provides an easy way to grab a few random files or release objects
// for many repositories at once without needing to interact with the github
// API directly.
//
// The performance for depends on how you are using it.
package githubfs

import (
	"compress/gzip"
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	gql "github.com/hasura/go-graphql-client"
)

// General structure:
// org/
// └── repo
//     ├── git
//     │   └── branch
//     │       └── files
//     ├── packages
//     │   └── container
//     └── releases
//         └── v0.0.1
//             ├── description.md
//             ├── file-0.0.1.tar.gz
//             └── sha256sum.txt
//
//  Depth:
//  0   1    2   3
//  org/repo/git/branch/...
//          /packages/container/...
//          /releases/{tag}/files/...

const (
	dirNameGit      = "git"
	dirNameReleases = "releases"
	//dirNamePackages = "packages"	// Add when supported.
)

const (
	ghModeFile       = 0100644
	ghModeExecutable = 0100755
	ghModeSubmodule  = 0160000
	ghModeSymlink    = 0120000
	ghModeDirectory  = 0040000
)

type input struct {
	org           string
	repo          string
	branch        string
	allowArchived bool
}

// ensure the FS matches the interface
var _ fs.FS = (*FS)(nil)

// FS provides the githubfs
type FS struct {
	httpClient  *http.Client
	gqlClient   *gql.Client
	connected   bool
	githubUrl   string
	rawUrl      string
	inputs      []input
	threshold   int
	root        *dir
	getGitDirFn func(*FS, *dir) error
}

// Option is the type used for options.
type Option func(gfs *FS)

// WithHttpClient provides a way to set the HTTP client to use.
func WithHttpClient(c *http.Client) Option {
	return func(gfs *FS) {
		gfs.httpClient = c
	}
}

// WithOrg instructs the filesystem to include all the repositories owned
// by an organization or user and the default branches of each repo.
//
// Repos marked as archived are filtered unless allowArchivedrepos is set to
// true.
func WithOrg(org string, allowArchivedrepos ...bool) Option {
	with := false
	if len(allowArchivedrepos) > 0 {
		with = allowArchivedrepos[len(allowArchivedrepos)-1]
	}
	return func(gfs *FS) {
		gfs.inputs = append(gfs.inputs, input{org: org, allowArchived: with})
	}
}

// WithRepo configures a specific owner, repository and branches to include.  If
// no branch is specified the default branch is selected.  Multiple branches may
// be specified in one call.
func WithRepo(org string, repo string, branches ...string) Option {
	if len(branches) == 0 {
		return func(gfs *FS) {
			gfs.inputs = append(gfs.inputs, input{
				org:           org,
				repo:          repo,
				allowArchived: true,
			})
		}
	}

	return func(gfs *FS) {
		for _, branch := range branches {
			gfs.inputs = append(gfs.inputs, input{
				org:           org,
				repo:          repo,
				branch:        branch,
				allowArchived: true,
			})
		}
	}
}

// WithSlug provides a way to easily configure a set of repos, or unique repo
// based on the slug string.
//
// slug = "org" 			 (the entire organization with default branch)
// slug = "org/repo" 		 (the exact repository with default branch)
// slug = "org/repo:branch"	 (the exact repository with specific branch)
//
// Repos marked as archived are filtered unless allowArchivedrepos is set to
// true.
func WithSlug(slug string, allowArchivedrepos ...bool) Option {
	var org, repo, branch string
	list := strings.Split(slug, "/")
	org = list[0]
	if len(list) > 1 && len(list[1]) > 0 {
		repo = list[1]
		list = strings.Split(repo, ":")
		repo = list[0]
		if len(list) > 1 && len(list[1]) > 0 {
			branch = list[1]
		}
	}

	allowArchived := false
	if len(allowArchivedrepos) > 0 {
		allowArchived = allowArchivedrepos[len(allowArchivedrepos)-1]
	}
	return func(gfs *FS) {
		gfs.inputs = append(gfs.inputs, input{
			org:           org,
			repo:          repo,
			branch:        branch,
			allowArchived: allowArchived,
		})
	}
}

// WithSlugs provides a way to pass in an array of slugs and it will take care
// of the rest.  Works like WithSlug() except no option for archived repos.
func WithSlugs(slugs ...string) Option {
	var opts []Option

	for _, slug := range slugs {
		opts = append(opts, WithSlug(slug))
	}

	return func(gfs *FS) {
		for _, opt := range opts {
			opt(gfs)
		}
	}
}

// WithThresholdInKB sets the maximum size to download the entire repository vs.
// downloading the individual files.
//
// Defaults to downloading a repo snapshot if the repo is less than 10MB.
func WithThresholdInKB(max int) Option {
	return func(gfs *FS) {
		gfs.threshold = max
	}
}

// WithGithubEnterprise specifies the API version to support for backwards
// compatibility.  The version value should be "3.3", "3.4", "3.5", "3.6", etc.
// The baseURL passed in should look like this:
//
// http://github.company.com
//
// The needed paths will be added to the baseURL.
//
// GHEC should not use this option as it uses the public API and hosting.
func WithGithubEnterprise(baseURL, version string) Option {
	switch version {
	default:
		return func(gfs *FS) {
			gfs.githubUrl = baseURL + "/api/graphql"
			gfs.rawUrl = baseURL + "/raw"
			gfs.getGitDirFn = getGitDirV3_3
		}
	}
}

// withTestURL provides a way to inject a test url.
func withTestURL(url string) Option {
	return func(gfs *FS) {
		gfs.githubUrl = url
		gfs.rawUrl = url
	}
}

// New creates a new githubfs.FS object with the specified configuration.
func New(opts ...Option) *FS {
	tenMB := 10 * 1024
	gfs := FS{
		httpClient:  http.DefaultClient,
		githubUrl:   "https://api.github.com/graphql",
		rawUrl:      "https://raw.githubusercontent.com",
		threshold:   tenMB,
		getGitDirFn: getGitDir,
	}

	for _, opt := range opts {
		opt(&gfs)
	}

	gfs.gqlClient = gql.NewClient(gfs.githubUrl, gfs.httpClient)
	gfs.root = newDir(&gfs, ".")

	return &gfs
}

// Open opens the named file.
func (gfs *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, fmt.Errorf("open %s %w", name, fs.ErrInvalid)
	}

	if err := gfs.connect(); err != nil {
		return nil, fmt.Errorf("open %s error connecting: %w", name, err)
	}

	child, err := gfs.get(name)
	if err != nil {
		return nil, fmt.Errorf("open %s error fetching file: %w", name, err)
	}

	switch child := child.(type) {
	case *file:
		return child.newFileHandle()
	case *dir:
		return child.newDirHandle(), nil
	}

	return nil, fmt.Errorf("open %s unexpected file type", name)
}

// connect is a helper function that connects to github and figures out the
// repositories that should be included in the file system.
func (gfs *FS) connect() error {
	if gfs.connected {
		return nil
	}

	// Fetch the bulk things first, so specific repos with extra details
	// can be added afterwards safely.
	for _, s := range gfs.inputs {
		if len(s.repo) == 0 {
			if err := gfs.fetchRepos(s); err != nil {
				return err
			}
		}
	}
	for _, s := range gfs.inputs {
		if len(s.repo) != 0 {
			if err := gfs.fetchRepo(s); err != nil {
				return err
			}
		}
	}
	gfs.connected = true
	return nil
}

// get fetches a directory or file by it's path.
func (gfs *FS) get(path string) (any, error) {
	if path == "." {
		return gfs.root, nil
	}

	dir, file, err := gfs.root.find(path)
	if err != nil {
		return nil, err
	}
	if file != nil {
		return file, nil
	}
	return dir, nil
}

// newRepo creates a new repo structure if it isn't present already.  Each needed
// node is created and linked.  The resulting nodes are returned by a map.
func (gfs *FS) newRepo(org, repo, branch string, releases, packages bool, size int) {
	o := gfs.root.mkdir(org, withOrg(org), notInPath())
	r := o.mkdir(repo, withRepo(repo), notInPath())
	if releases {
		r.mkdir(dirNameReleases, withFetcher(getReleaseDir), notInPath())
	}
	//if packages {
	//	// Add when we can get the data via graphql.
	//	r.mkdir(dirNamePackages, withFetcher(nil))
	//}

	git := r.mkdir(dirNameGit, notInPath())

	if len(branch) > 0 {
		opt := withFetcher(gfs.getGitDirFn)
		if size <= gfs.threshold {
			opt = withFetcher(getEntireGitDir)
		}
		git.mkdir(branch, withBranch(branch), notInPath(), opt)
	}
}

// fetchRepo calls github and asks for a single specific repo, and links it
// back to the filesystem.
func (gfs *FS) fetchRepo(s input) (err error) {
	vars := map[string]any{
		"owner": s.org,
		"repo":  s.repo,
	}

	var query struct {
		Repo struct {
			DiskUsage        int
			IsArchived       bool
			IsDisabled       bool
			NameWithOwner    string
			DefaultBranchRef struct {
				Name string
			}
			Releases struct {
				TotalCount int
			}
		} `graphql:"repository(name: $repo, owner: $owner)"`
	}

	if err = gfs.gqlClient.Query(context.Background(), &query, vars); err != nil {
		return err
	}

	if !s.allowArchived && query.Repo.IsArchived ||
		query.Repo.IsDisabled ||
		query.Repo.NameWithOwner != s.org+"/"+s.repo {
		return nil
	}

	branch := s.branch
	if len(branch) == 0 {
		branch = query.Repo.DefaultBranchRef.Name
	}
	releases := query.Repo.Releases.TotalCount > 0
	size := query.Repo.DiskUsage
	gfs.newRepo(s.org, s.repo, branch, releases, false, size)

	return nil
}

// fetchRepos calls githun and asks for all the repos in an org/user space, and
// links them back to the filesystem.
func (gfs *FS) fetchRepos(s input) (err error) {
	vars := map[string]any{
		"owner": s.org,
		"count": 100,
		"after": (*string)(nil),
	}

	more := true
	for more {
		var query struct {
			Owner struct {
				Repo struct {
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
					Edges []struct {
						Node struct {
							Name             string
							DiskUsage        int
							IsArchived       bool
							IsDisabled       bool
							NameWithOwner    string
							DefaultBranchRef struct {
								Name string
							}
							Releases struct {
								TotalCount int
							}
						}
					}
				} `graphql:"repositories(orderBy: {field: NAME, direction: ASC}, first: $count, after: $after)"`
			} `graphql:"repositoryOwner(login: $owner)"`
		}

		if err = gfs.gqlClient.Query(context.Background(), &query, vars); err != nil {
			return err
		}

		for _, edge := range query.Owner.Repo.Edges {
			if !s.allowArchived && edge.Node.IsArchived ||
				edge.Node.IsDisabled ||
				edge.Node.NameWithOwner != s.org+"/"+edge.Node.Name {
				continue
			}

			branch := edge.Node.DefaultBranchRef.Name
			releases := edge.Node.Releases.TotalCount > 0
			size := edge.Node.DiskUsage
			gfs.newRepo(s.org, edge.Node.Name, branch, releases, false, size)
		}

		more = query.Owner.Repo.PageInfo.HasNextPage
		vars["after"] = query.Owner.Repo.PageInfo.EndCursor
	}

	return nil
}

// getEntireGitDir fetches the entire directory as a tarball and decodes the
// result into the filesystem subtree.  For small repos this is much faster.
func getEntireGitDir(gfs *FS, d *dir) error {
	vars := map[string]any{
		"owner":  d.org,
		"repo":   d.repo,
		"branch": "refs/heads/" + d.branch,
	}

	/*
	   query {
	     repository(name: "repo", owner: "org") {
	       ref(qualifiedName: "refs/heads/main") {
	         target {
	           ... on Commit {
	             tarballUrl
	           }
	         }
	       }
	     }
	   }
	*/
	var query struct {
		Repo struct {
			Ref struct {
				Target struct {
					Commit struct {
						TarballUrl string
					} `graphql:"... on Commit"`
				}
			} `graphql:"ref(qualifiedName: $branch)"`
		} `graphql:"repository(name: $repo, owner: $owner)"`
	}

	if err := gfs.gqlClient.Query(context.Background(), &query, vars); err != nil {
		return err
	}

	resp, err := gfs.httpClient.Get(query.Repo.Ref.Target.Commit.TarballUrl)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("http status code not 200: %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	bodyReader := resp.Body
	switch ct {
	case "application/x-gzip", "application/gzip":
		if !resp.Uncompressed {
			zr, err := gzip.NewReader(bodyReader)
			if err != nil {
				return err
			}
			bodyReader = zr
		}
	case "application/octet-stream", "application/x-tar":
		// Use the stream without unzipping.
	default:
		return fmt.Errorf("unsupported content type: %s", ct)
	}

	return d.tarballToTree(bodyReader)
}

// getGitDir fetches a single directory via the github API. This isn't fast, but
// there are conditions where it is advantageous over fetching everything all at
// once.
func getGitDir(gfs *FS, d *dir) error {
	path := strings.Join(d.path, "/")

	vars := map[string]any{
		"owner": d.org,
		"repo":  d.repo,
		"exp":   d.branch + ":" + path,
	}

	/*
		query {
		  repository(name: "repo", owner: "org") {
		    object(expression: "main:") {
		      ... on Tree {
		        entries {
		          name
		          size
		          mode
		        }
		      }
		    }
		  }
		}
	*/
	var query struct {
		Repository struct {
			Object struct {
				Tree struct {
					Entries []struct {
						Name string
						Size int
						Mode int
					}
				} `graphql:"... on Tree"`
			} `graphql:"object(expression: $exp)"`
		} `graphql:"repository(name: $repo, owner: $owner)"`
	}

	if err := gfs.gqlClient.Query(context.Background(), &query, vars); err != nil {
		return err
	}

	for _, entry := range query.Repository.Object.Tree.Entries {
		url := strings.Join([]string{gfs.rawUrl, d.org, d.repo, d.branch, path, entry.Name}, "/")

		switch entry.Mode {
		case ghModeFile:
			d.addFile(entry.Name, withUrl(url), withSize(entry.Size))
		case ghModeExecutable:
			d.addFile(entry.Name, withUrl(url), withSize(entry.Size), withMode(fs.FileMode(0755)))
		case ghModeDirectory:
			d.newDir(entry.Name, withFetcher(getGitDir))
		case ghModeSubmodule: // TODO
		case ghModeSymlink: // TODO
		default:
			return fmt.Errorf("unknown file mode")
		}
	}

	return nil
}

// getReleaseDir fetches the release information and makes it into a directory
// structure that is linked to the filesystem
func getReleaseDir(gfs *FS, d *dir) error {
	vars := map[string]any{
		"owner": d.org,
		"repo":  d.repo,
		"count": 100,
		"after": (*string)(nil),
	}

	/*	query MyQuery {
		  repository(name: "repo", owner: "org") {
		    releases(first: 10, orderBy: {field: CREATED_AT, direction: DESC}) {
		      edges {
		        node {
		          tag {
		            name
		          }
		          isPrerelease
		          isDraft
				  createdAt
		          description
		          releaseAssets(first: 10) {
		            edges {
		              node {
		                downloadUrl
		                name
		                size
		              }
		            }
		          }
		        }
		      }
		    }
		  }
		}
	*/
	more := true
	for more {
		var query struct {
			Repository struct {
				Releases struct {
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
					Edges []struct {
						Node struct {
							Tag struct {
								Name string
							}
							IsPrerelease  bool
							IsDraft       bool
							CreatedAt     string
							Description   string
							ReleaseAssets struct {
								Edges []struct {
									Node struct {
										DownloadUrl string
										Name        string
										Size        int
									}
								}
							} `graphql:"releaseAssets(first:100)"`
						}
					}
				} `graphql:"releases(first: $count, orderBy: {field: CREATED_AT, direction: DESC}, after: $after)"`
			} `graphql:"repository(name: $repo, owner: $owner)"`
		}

		if err := gfs.gqlClient.Query(context.Background(), &query, vars); err != nil {
			return err
		}

		for _, edge := range query.Repository.Releases.Edges {
			if edge.Node.IsDraft || edge.Node.IsPrerelease {
				continue
			}

			tag := edge.Node.Tag.Name
			desc := edge.Node.Description

			relDir := d.newDir(tag)

			relDir.addFile("description.md", withContent([]byte(desc)))

			for _, asset := range edge.Node.ReleaseAssets.Edges {
				relDir.addFile(asset.Node.Name,
					withSize(asset.Node.Size),
					withUrl(asset.Node.DownloadUrl))
			}
		}

		more = query.Repository.Releases.PageInfo.HasNextPage
		vars["after"] = query.Repository.Releases.PageInfo.EndCursor
	}

	return nil
}
