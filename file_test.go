// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package githubfs

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFile(t *testing.T) {
	parent := &dir{
		org:  "org",
		repo: "repo",
	}
	now := time.Now()
	content := []byte("hello, world")
	tests := []struct {
		description   string
		name          string
		opts          []fileOpt
		expectSize    int
		expectMode    fs.FileMode
		expectTime    time.Time
		expectContent []byte
		expectUrl     string
	}{
		{
			description: "simple test",
			name:        "foo",
			expectMode:  fs.FileMode(0644),
		}, {
			description:   "with content",
			name:          "foo",
			opts:          []fileOpt{withContent(content)},
			expectSize:    len(content),
			expectContent: content,
			expectMode:    fs.FileMode(0644),
		}, {
			description: "with mod time",
			name:        "foo",
			opts:        []fileOpt{withModTime(now)},
			expectTime:  now,
			expectMode:  fs.FileMode(0644),
		}, {
			description: "with file type",
			name:        "foo",
			opts:        []fileOpt{withMode(0755)},
			expectMode:  fs.FileMode(0755),
		}, {
			description: "with file type",
			name:        "foo",
			opts:        []fileOpt{withSize(10)},
			expectMode:  fs.FileMode(0644),
			expectSize:  10,
		}, {
			description:   "with content and size",
			name:          "foo",
			opts:          []fileOpt{withContent(content), withSize(1000)},
			expectSize:    len(content),
			expectContent: content,
			expectMode:    fs.FileMode(0644),
		}, {
			description:   "with content and size, reversed order",
			name:          "foo",
			opts:          []fileOpt{withSize(1000), withContent(content)},
			expectSize:    len(content),
			expectContent: content,
			expectMode:    fs.FileMode(0644),
		}, {
			description: "with url",
			name:        "foo",
			opts:        []fileOpt{withUrl("foobar")},
			expectUrl:   "foobar",
			expectMode:  fs.FileMode(0644),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			assert := assert.New(t)

			f := newFile(parent, tc.name, tc.opts...)
			assert.NotNil(f)

			assert.Equal("org", f.owner)
			assert.Equal("repo", f.repo)
			assert.Equal(int64(tc.expectSize), f.info.size)
			assert.Equal(tc.expectMode, f.info.mode)
			assert.Equal(tc.expectTime, f.info.modTime)
			assert.Equal(tc.expectUrl, f.url)
		})
	}
}

func TestToDirEntry(t *testing.T) {
	parent := &dir{
		org:  "org",
		repo: "repo",
	}

	assert := assert.New(t)

	f := newFile(parent, "name")
	assert.NotNil(f)

	de := f.toDirEntry()
	assert.Equal("name", de.Name())
}

func TestNewFileHandle(t *testing.T) {
	tests := []struct {
		description string
		name        string
		opts        []fileOpt
		payload     string
		statusCode  int
		expectErr   bool
	}{
		{
			description: "simple test",
			name:        "file_1",
			payload:     "file_1 payload",
		}, {
			description: "simple 404 test",
			name:        "file_1",
			payload:     "file_1 payload",
			statusCode:  404,
			expectErr:   true,
		}, {
			description: "simple empty test",
			name:        "file_1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.statusCode != 0 {
					w.WriteHeader(tc.statusCode)
				}
				fmt.Fprintf(w, tc.payload)
			}))

			gfs := FS{
				httpClient: &http.Client{},
			}

			parent := dir{
				gfs:  &gfs,
				org:  "org",
				repo: "repo",
			}

			f := newFile(&parent, tc.name, withUrl(server.URL), withSize(10))
			require.NotNil(f)

			got, err := f.newFileHandle()

			if !tc.expectErr {
				assert.NoError(err)
				assert.NotNil(got)

				assert.Equal(int64(len(tc.payload)), got.info.Size())

				if len(tc.payload) > 0 {
					b := make([]byte, 50)

					n, err := got.Read(b)
					assert.NoError(err)
					assert.Equal(len(tc.payload), n)
					assert.Equal(string(b[:n]), tc.payload)
				}
			} else {
				assert.Error(err)
				assert.Nil(got)
			}

			server.Close()
		})
	}
}
