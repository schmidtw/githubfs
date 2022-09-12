// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package githubfs

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// dir represents a directory node.
type dir struct {
	m        sync.Mutex
	gfs      *FS
	parent   *dir
	org      string
	repo     string
	name     string
	branch   string
	path     []string
	perm     os.FileMode
	modTime  time.Time
	children map[string]any
	fetchFn  func(*FS, *dir) error
}

type dirOpt func(d *dir)

// withOrg provides a way to set the org for the directory.
func withOrg(org string) dirOpt {
	return func(d *dir) {
		d.org = org
	}
}

// withRepo provides a way to set the repo for the directory.
func withRepo(repo string) dirOpt {
	return func(d *dir) {
		d.repo = repo
	}
}

// withBranch provides a way to set the branch for the directory.
func withBranch(branch string) dirOpt {
	return func(d *dir) {
		d.branch = branch
	}
}

// withFetcher provides a way to set a fetcher to populate the directory lazily.
func withFetcher(fn func(*FS, *dir) error) dirOpt {
	return func(d *dir) {
		d.fetchFn = fn
	}
}

// notInPath provides a way to exclude this directory from being used for general
// path determination.  Generally only something done at the org/repo/git levels.
func notInPath() dirOpt {
	return func(d *dir) {
		d.path = []string{}
	}
}

// withDirModTime provides a way to set the modification time of the directory.
func withDirModTime(t time.Time) dirOpt {
	return func(d *dir) {
		d.modTime = t
	}
}

// newDir creates a new directory based on the specified filesystem.  Really
// only useful when creating the root node.  Use (*dir).newDir() normally.
func newDir(gfs *FS, name string, opts ...dirOpt) *dir {
	n := dir{
		gfs:      gfs,
		name:     name,
		perm:     fs.ModeDir | 0755,
		children: make(map[string]any),
	}

	for _, opt := range opts {
		opt(&n)
	}

	return &n
}

// newDir creates a new directory node tied to the existing node.
func (d *dir) newDir(name string, opts ...dirOpt) *dir {
	n := dir{
		gfs:      d.gfs,
		parent:   d,
		path:     append(d.path, name),
		org:      d.org,
		repo:     d.repo,
		name:     name,
		branch:   d.branch,
		perm:     fs.ModeDir | 0755,
		children: make(map[string]any),
	}
	d.children[name] = &n

	for _, opt := range opts {
		opt(&n)
	}

	return &n
}

// mkdir makes the specified directory if it isn't already present and returns
// the directory, or returns the found directory.
func (d *dir) mkdir(path string, opts ...dirOpt) *dir {
	return d.makeDirs([]string{path}, opts...)
}

// makeDirs makes the specified directories if it isn't already present and
// returns leaf directory, or returns the found directory.
func (d *dir) makeDirs(parts []string, opts ...dirOpt) *dir {
	next, found := d.children[parts[0]]
	if found {
		if len(parts) > 1 {
			return next.(*dir).makeDirs(parts[1:], opts...)
		}
		return next.(*dir)
	}

	rv := d.newDir(parts[0], opts...)
	d.children[parts[0]] = rv
	if len(parts) > 1 {
		return rv.makeDirs(parts[1:], opts...)
	}

	return rv
}

// newDirHandle creates a new dirHandle and returns it.
func (d *dir) newDirHandle() *dirHandle {
	d.m.Lock()
	defer d.m.Unlock()

	var entries []fs.DirEntry
	for _, child := range d.children {
		switch child := child.(type) {
		case *file:
			entries = append(entries, child.toDirEntry())
		case *dir:
			entries = append(entries, child.toDirEntry())
		}
	}

	return &dirHandle{
		info:    d.toFileInfo(),
		entries: entries,
	}
}

// toFileInfo returns a fileInfo object for this directory.
func (d *dir) toFileInfo() *fileInfo {
	return &fileInfo{
		name:    d.name,
		size:    4096,
		modTime: d.modTime,
		mode:    d.perm,
	}
}

// toDirEntry returns a dirEntry object for this directory.
func (d *dir) toDirEntry() *dirEntry {
	return &dirEntry{
		info: d.toFileInfo(),
	}
}

// addFile creates a new file object based on a specific dir object.
func (d *dir) addFile(name string, opts ...fileOpt) *file {
	f := newFile(d, name, opts...)
	d.children[name] = f
	return f
}

// fullPath provides he path back to the root node.
func (d *dir) fullPath() string {
	p := d

	var paths []string

	for p != nil {
		// Don't include the root directory name
		if p.parent != nil {
			paths = append([]string{p.name}, paths...)
		}
		p = p.parent
	}

	return strings.Join(paths, "/")
}

// tarballToTree converts a tarball into a complete filesystem tree.
func (d *dir) tarballToTree(tarball io.Reader) error {
	d.fetchFn = nil
	tr := tar.NewReader(tarball)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // end of archive
		}
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeReg:
			path, filename := filepath.Split(hdr.Name)
			parts := tarSplitPath(path)
			leaf := d
			if len(parts) > 0 {
				leaf = d.makeDirs(parts, withDirModTime(hdr.ModTime))
			}
			buf := new(bytes.Buffer)
			_, err := buf.ReadFrom(tr)
			if err != nil && err != io.EOF {
				return err
			}
			leaf.addFile(filename, withModTime(hdr.ModTime), withContent(buf.Bytes()))
		case tar.TypeDir:
			parts := tarSplitPath(hdr.Name)
			if len(parts) > 0 {
				d.makeDirs(parts, withDirModTime(hdr.ModTime))
			}
		case tar.TypeLink, tar.TypeSymlink:
			insertPoint := d.fullPath()
			targetPathOnly, _ := filepath.Split(hdr.Name)
			targetParts := tarSplitPath(targetPathOnly)
			target := filepath.Clean(insertPoint + "/" + strings.Join(targetParts, "/") + "/" + hdr.Linkname)
			linkname := filepath.Clean(insertPoint + "/" + strings.Join(tarSplitPath(hdr.Name), "/"))

			linknamePath, linknameFile := filepath.Split(linkname)
			linknamePath = filepath.Clean(linknamePath)

			targetDir, targetFile, err := d.gfs.root.find(target)
			if err != nil {
				return err
			}
			linknameDir, _, err := d.gfs.root.find(linknamePath)
			if err != nil {
				return err
			}
			if nil != targetFile {
				linknameDir.children[filepath.Base(linkname)] = targetFile
			} else {
				linknameDir.children[linknameFile] = targetDir
			}
		}
	}

	return nil
}

// fetch fetches the information about the directory and removes the fetch function
// so that it's not fetched again.
func (d *dir) fetch() error {
	if d.fetchFn != nil {
		err := d.fetchFn(d.gfs, d)
		if err != nil {
			return fmt.Errorf("githubfs filesystem error can't fetch a directory: %w", err)
		}
		d.fetchFn = nil
	}
	return nil
}

// findDir finds either the exact directory, or the directory containing
// the file specified.
func (d *dir) find(path string) (*dir, *file, error) {
	parts := strings.Split(path, "/")
	cur := d
	for i, part := range parts {
		if err := cur.fetch(); err != nil {
			return nil, nil, err
		}

		child, found := cur.children[part]
		if !found {
			return nil, nil, fmt.Errorf("directory %s not found %w", part, fs.ErrNotExist)
		}
		if _, isFile := child.(*file); isFile {
			if i+1 == len(parts) {
				return cur, child.(*file), nil
			}
			return nil, nil, fmt.Errorf("directory %s not found %w", part, fs.ErrNotExist)
		}
		cur = child.(*dir)
	}

	if err := cur.fetch(); err != nil {
		return nil, nil, err
	}

	return cur, nil, nil
}

// tarSplitPath cleans up the path by removing the leading directory and any
// trailing '/' characters that could cause issues.
func tarSplitPath(path string) []string {
	path = strings.TrimSuffix(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		// remove the prefix directory if present
		parts = parts[1:]
	}
	return parts
}
