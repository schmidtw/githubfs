// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package githubfs

import "io/fs"

// ensure the dirEntry matches the interface.
var _ fs.DirEntry = (*dirEntry)(nil)

type dirEntry struct {
	info fs.FileInfo
}

// Name returns the name of the file (or subdirectory) described by the entry.
// This name is only the final element of the path (the base name), not the entire path.
// For example, Name would return "hello.go" not "home/gopher/hello.go".
func (d *dirEntry) Name() string {
	return d.info.Name()
}

// IsDir reports whether the entry describes a directory.
func (d *dirEntry) IsDir() bool {
	return d.info.IsDir()
}

// Type returns the type bits for the entry.
// The type bits are a subset of the usual FileMode bits, those returned by the FileMode.Type method.
func (d *dirEntry) Type() fs.FileMode {
	return d.info.Mode()
}

// Info returns the FileInfo for the file or subdirectory described by the entry.
// The returned FileInfo may be from the time of the original directory read
// or from the time of the call to Info. If the file has been removed or renamed
// since the directory read, Info may return an error satisfying errors.Is(err, ErrNotExist).
// If the entry denotes a symbolic link, Info reports the information about the link itself,
// not the link's target.
func (d *dirEntry) Info() (fs.FileInfo, error) {
	// TODO sym link support
	return d.info, nil
}
