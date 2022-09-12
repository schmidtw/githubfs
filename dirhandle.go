// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package githubfs

import (
	"fmt"
	"io"
	"io/fs"
	"sync"
)

// ensure the dirHandle matches the interface.
var _ fs.ReadDirFile = (*dirHandle)(nil)

// dirHandle provides a directory with an index for supporting the ReadDir
// operation.  The index shows where in the list of files in the directory
// a walk operation is, so this can't be merged with the dir object.
type dirHandle struct {
	m       sync.Mutex
	info    fs.FileInfo
	entries []fs.DirEntry
	index   int
	closed  bool
}

// Stat returns a FileInfo describing the file.
func (d *dirHandle) Stat() (fs.FileInfo, error) {
	d.m.Lock()
	defer d.m.Unlock()
	if d.closed {
		return nil, fmt.Errorf("stat %s %w", d.info.Name(), fs.ErrClosed)
	}

	return d.info, nil
}

// Read fulfills the fs.File requirement.
func (d *dirHandle) Read(b []byte) (int, error) {
	d.m.Lock()
	defer d.m.Unlock()
	if d.closed {
		return 0, fmt.Errorf("stat %s %w", d.info.Name(), fs.ErrClosed)
	}

	return 0, fmt.Errorf("is a directory not a file")
}

// Close fulfills the fs.File requirement.
func (d *dirHandle) Close() error {
	d.m.Lock()
	defer d.m.Unlock()
	if d.closed {
		return fmt.Errorf("stat %s %w", d.info.Name(), fs.ErrClosed)
	}

	d.closed = true
	return nil
}

// ReadDir reads the contents of the directory and returns
// a slice of up to n DirEntry values in directory order.
// Subsequent calls on the same file will yield further DirEntry values.
//
// If n > 0, ReadDir returns at most n DirEntry structures.
// In this case, if ReadDir returns an empty slice, it will return
// a non-nil error explaining why.
// At the end of a directory, the error is io.EOF.
// (ReadDir must return io.EOF itself, not an error wrapping io.EOF.)
//
// If n <= 0, ReadDir returns all the DirEntry values from the directory
// in a single slice. In this case, if ReadDir succeeds (reads all the way
// to the end of the directory), it returns the slice and a nil error.
// If it encounters an error before the end of the directory,
// ReadDir returns the DirEntry list read until that point and a non-nil error.
func (d *dirHandle) ReadDir(n int) ([]fs.DirEntry, error) {
	d.m.Lock()
	defer d.m.Unlock()
	if d.closed {
		return nil, fmt.Errorf("stat %s %w", d.info.Name(), fs.ErrClosed)
	}

	have := d.entries[d.index:]
	if len(have) == 0 {
		return have, io.EOF
	}

	l := len(d.entries)

	if n <= 0 {
		d.index = l - 1
		return have, nil
	}

	var rv []fs.DirEntry
	for n > 0 && d.index < l {
		rv = append(rv, d.entries[d.index])
		n--
		d.index++
	}

	if n == 0 {
		return rv, nil
	}
	return rv, io.EOF
}
