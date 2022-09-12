// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package githubfs

import (
	"fmt"
	"io"
	"io/fs"
	"sync"
	"time"
)

// file provides the concrete fs.File implementation for the filesystem.
type file struct {
	m       sync.Mutex
	gfs     *FS
	parent  *dir
	owner   string
	repo    string
	info    fileInfo
	url     string
	content []byte
}

type fileOpt func(f *file)

func withContent(content []byte) fileOpt {
	return func(f *file) {
		f.content = content
		f.info.size = int64(len(content))
	}
}

func withUrl(url string) fileOpt {
	return func(f *file) {
		f.url = url
	}
}

func withModTime(t time.Time) fileOpt {
	return func(f *file) {
		f.info.modTime = t
	}
}

func withMode(mode fs.FileMode) fileOpt {
	return func(f *file) {
		f.info.mode = mode
	}
}

func withSize(size int) fileOpt {
	return func(f *file) {
		if int64(len(f.content)) == 0 {
			f.info.size = int64(size)
		}
	}
}

func newFile(parent *dir, name string, opts ...fileOpt) *file {
	f := file{
		gfs:    parent.gfs,
		parent: parent,
		owner:  parent.org,
		repo:   parent.repo,
		info: fileInfo{
			name: name,
			mode: fs.FileMode(0644),
		},
	}

	for _, opt := range opts {
		opt(&f)
	}

	return &f
}

func (f *file) newFileHandle() (*fileHandle, error) {
	f.m.Lock()
	defer f.m.Unlock()

	if int64(len(f.content)) != f.info.size {
		resp, err := f.gfs.httpClient.Get(f.url)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("http status code not 200: %d\n", resp.StatusCode)
		}
		bod, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		f.content = bod
		f.info.size = int64(len(bod))
	}

	return newFileHandle(f.info, f.content), nil
}

func (f *file) toDirEntry() *dirEntry {
	f.m.Lock()
	defer f.m.Unlock()

	return &dirEntry{
		info: &f.info,
	}
}
