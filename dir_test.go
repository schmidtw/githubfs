// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package githubfs

import (
	"bytes"
	_ "embed"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDir(t *testing.T) {
	now := time.Now()
	tests := []struct {
		description   string
		name          string
		opts          []dirOpt
		expectOrg     string
		expectRepo    string
		expectBranch  string
		expectFetchFn bool
		expectMode    os.FileMode
		expectTime    time.Time
		expectPath    []string
	}{
		{
			description: "simple test",
			name:        "foo",
			expectPath:  []string{"foo"},
		}, {
			description: "withDirModTime() test",
			name:        "bar",
			opts:        []dirOpt{withDirModTime(now)},
			expectTime:  now,
			expectPath:  []string{"bar"},
		}, {
			description: "notInPath() test",
			name:        "bar",
			opts:        []dirOpt{notInPath()},
			expectPath:  []string{},
		}, {
			description:   "withFetcher() test",
			name:          "bar",
			opts:          []dirOpt{withFetcher(func(_ *FS, _ *dir) error { return nil })},
			expectFetchFn: true,
			expectPath:    []string{"bar"},
		}, {
			description:  "withBranch() test",
			name:         "bar",
			opts:         []dirOpt{withBranch("main")},
			expectBranch: "main",
			expectPath:   []string{"bar"},
		}, {
			description: "withRepo() test",
			name:        "bar",
			opts:        []dirOpt{withRepo("flowers")},
			expectRepo:  "flowers",
			expectPath:  []string{"bar"},
		}, {
			description: "withOrg() test",
			name:        "bar",
			opts:        []dirOpt{withOrg("red")},
			expectOrg:   "red",
			expectPath:  []string{"bar"},
		}, {
			description:  "several things test",
			name:         "bar",
			opts:         []dirOpt{withOrg("red"), withRepo("flowers"), withBranch("wood")},
			expectOrg:    "red",
			expectRepo:   "flowers",
			expectBranch: "wood",
			expectPath:   []string{"bar"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			base := newDir(nil, ".")
			require.NotNil(base)
			assert.Equal(fs.ModeDir|0755, base.perm)
			assert.Equal(0, len(base.children))

			sub := base.newDir(tc.name, tc.opts...)
			require.NotNil(sub)

			assert.Equal(sub.parent, base)
			assert.Equal(tc.name, sub.name)
			assert.Equal(tc.expectRepo, sub.repo)
			assert.Equal(tc.expectOrg, sub.org)
			assert.Equal(tc.expectBranch, sub.branch)
			if tc.expectMode == 0 {
				assert.Equal(fs.ModeDir|0755, sub.perm)
			} else {
				assert.Equal(tc.expectMode, sub.perm)
			}
			if tc.expectFetchFn {
				assert.NotNil(sub.fetchFn)
			} else {
				assert.Nil(sub.fetchFn)
			}
			assert.Equal(1, len(base.children))
			assert.NotNil(base.children[tc.name])
			assert.True(reflect.DeepEqual(tc.expectPath, sub.path))
		})
	}
}

func TestDirToDirEntry(t *testing.T) {
	now := time.Now()
	parent := newDir(nil, "name", withDirModTime(now))

	assert := assert.New(t)

	de := parent.toDirEntry()
	assert.Equal("name", de.Name())
	info, _ := de.Info()
	assert.Equal(now, info.ModTime())
	assert.Equal(int64(4096), info.Size())
	assert.True(de.IsDir())
	assert.Equal(parent.perm, de.Type())
}

func TestNewDirHandle(t *testing.T) {
	// it doesn't matter that the parent is technically wrong for this test
	a := newDir(nil, "a")
	b := newDir(nil, "b")
	c := newDir(nil, "c")
	d := newFile(c, "d")
	tests := []struct {
		description string
		dir         *dir
		expectNames []string
	}{
		{
			description: "simple test",
			dir: &dir{
				name: "e",
				children: map[string]any{
					"a": a,
					"b": b,
					"c": c,
					"d": d,
				},
			},
			expectNames: []string{"a", "b", "c", "d"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			got := tc.dir.newDirHandle()

			require.NotNil(got)

			require.Equal(len(tc.expectNames), len(got.entries))
			sort.Strings(tc.expectNames)

			var have []string
			for i := range tc.expectNames {
				have = append(have, got.entries[i].Name())
			}
			sort.Strings(have)
			assert.True(reflect.DeepEqual(tc.expectNames, have))
		})
	}
}

func TestMakeDirs(t *testing.T) {
	assert := assert.New(t)

	parent := newDir(nil, ".")

	c := parent.makeDirs([]string{"a", "b", "c"})

	assert.Equal("c", c.name)
	assert.True(reflect.DeepEqual([]string{"a", "b", "c"}, c.path))

	c2 := parent.makeDirs([]string{"a", "b", "c"})
	assert.True(reflect.DeepEqual([]string{"a", "b", "c"}, c2.path))

	d := parent.makeDirs([]string{"a", "b", "d"})
	assert.True(reflect.DeepEqual([]string{"a", "b", "d"}, d.path))

	e := parent.mkdir("e")
	assert.True(reflect.DeepEqual([]string{"e"}, e.path))
}

func TestAddFile(t *testing.T) {
	assert := assert.New(t)

	parent := newDir(nil, ".")

	f := parent.addFile("file")
	assert.NotNil(f)
	assert.Equal(parent.children["file"], f)
}

//go:embed tarballs/simple.tar
var simpleTar []byte

//go:embed tarballs/symlinks.tar
var symlinksTar []byte

//go:embed tarballs/badlink.tar
var badlinkTar []byte

func TestTarballToTree(t *testing.T) {

	type entry struct {
		path    string
		isDir   bool
		content string
	}
	tests := []struct {
		description   string
		tarball       []byte
		expectEntries []entry
		expectErr     error
	}{
		{
			description: "a simple test",
			tarball:     simpleTar,
			expectEntries: []entry{
				{path: "1/2/a", content: "a\n"},
				{path: "1/2/b", content: "b\n"},
				{path: "1/2/c", isDir: true},
				{path: "1/2/c/d", content: "d\n"},
			},
		}, {
			description: "a test with symlinks",
			tarball:     symlinksTar,
			expectEntries: []entry{
				{path: "1/2/a", content: "a\n"},
				{path: "1/2/b", content: "b\n"},
				{path: "1/2/c", isDir: true},
				{path: "1/2/c/b", content: "b\n"},
				{path: "1/2/c/d", content: "d\n"},
				{path: "1/2/c/w", content: "a\n"},
				{path: "1/2/e", isDir: true},
				{path: "1/2/e/b", content: "b\n"},
				{path: "1/2/e/d", content: "d\n"},
				{path: "1/2/e/w", content: "a\n"},
			},
		}, {
			description: "a file with a bad symlink",
			tarball:     badlinkTar,
			expectErr:   fs.ErrNotExist,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			gfs := &FS{}
			gfs.root = newDir(gfs, ".")

			parent := gfs.root.newDir("1").newDir("2")
			b := bytes.NewBuffer(tc.tarball)

			err := parent.tarballToTree(b)

			if tc.expectErr == nil {
				assert.NoError(err)

				for _, entry := range tc.expectEntries {
					d, f, err := gfs.root.find(entry.path)
					assert.NoError(err)
					if entry.isDir {
						assert.NotNil(d)
						assert.Nil(f)
					} else {
						assert.NotNil(d)
						require.NotNil(f)
						assert.Equal([]byte(entry.content), f.content)
					}
				}
				return
			}
			assert.Error(err)
		})
	}
}

func TestFetchAndFind(t *testing.T) {
	forcedErr := fmt.Errorf("error")
	tests := []struct {
		description string
		expectErr   error
		path        string
		fn          func(*FS, *dir) error
	}{
		{
			description: "a simple test",
			path:        "1/2/d",
		}, {
			description: "no directory there",
			path:        "1/2/3",
			expectErr:   fs.ErrNotExist,
		}, {
			description: "treating a file like a directory.",
			path:        "1/2/d/3",
			expectErr:   fs.ErrNotExist,
		}, {
			description: "a failed fetch.",
			path:        "1/2/d",
			fn:          func(_ *FS, _ *dir) error { return forcedErr },
			expectErr:   forcedErr,
		}, {
			description: "a failed fetch on last directory",
			path:        "1/2",
			fn:          func(_ *FS, _ *dir) error { return forcedErr },
			expectErr:   forcedErr,
		}, {
			description: "a successful fetch.",
			path:        "1/2/d",
			fn:          func(_ *FS, _ *dir) error { return nil },
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			assert := assert.New(t)
			gfs := &FS{}
			gfs.root = newDir(gfs, ".")

			parent := gfs.root.newDir("1").newDir("2", withFetcher(tc.fn))
			parent.addFile("d")

			d, f, err := gfs.root.find(tc.path)

			if tc.expectErr == nil {
				assert.NoError(err)
				assert.NotNil(d)
				return
			}

			assert.Nil(d)
			assert.Nil(f)
			assert.ErrorIs(err, tc.expectErr)
		})
	}
}

/* Handy debugging too.
func ls(d *dir, path string) {
	for name, child := range d.children {
		switch child := child.(type) {
		case *file:
			fmt.Printf("%s/%s\n", path, name)
		case *dir:
			fmt.Printf("%s/%s/\n", path, name)
			next := path + "/" + name
			ls(child, next)
		}
	}
}
*/
