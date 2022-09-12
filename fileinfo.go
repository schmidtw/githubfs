// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package githubfs

import (
	"io/fs"
	"time"
)

// ensure the fileInfo matches the interface.
var _ fs.FileInfo = (*fileInfo)(nil)

// fileInfo describes a file and is returned by Stat.
type fileInfo struct {
	name    string
	size    int64
	modTime time.Time
	mode    fs.FileMode
}

// Name returns the base name of the file.
func (fi *fileInfo) Name() string {
	return fi.name
}

// Size returns the length in bytes for regular files; system-dependent for others.
func (fi *fileInfo) Size() int64 {
	return fi.size
}

// Mode returns the file mode bits.
func (fi *fileInfo) Mode() fs.FileMode {
	return fi.mode
}

// ModTime returns the modification time.
func (fi *fileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir returns the abbreviation for Mode().IsDir().
func (fi *fileInfo) IsDir() bool {
	return fi.mode&fs.ModeDir > 0
}

// Sys returns the underlying data source (can return nil).  (Always nil).
func (fi *fileInfo) Sys() any {
	return nil
}
