// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package githubfs

import (
	"bytes"
	"fmt"
	"io/fs"
	"sync"
)

// ensure the File matches the interface
var _ fs.File = (*fileHandle)(nil)

// fileHandle is the external file given out that can be read and closed.
type fileHandle struct {
	m       sync.Mutex
	info    fileInfo
	content *bytes.Buffer
	closed  bool
}

func newFileHandle(info fileInfo, content []byte) *fileHandle {
	return &fileHandle{
		info:    info,
		content: bytes.NewBuffer(content),
	}
}

// Stat returns a FileInfo describing the file.
func (f *fileHandle) Stat() (fs.FileInfo, error) {
	f.m.Lock()
	defer f.m.Unlock()
	if f.closed {
		return nil, fmt.Errorf("stat %s %w", f.info.name, fs.ErrClosed)
	}

	return &f.info, nil
}

// Read reads up to len(b) bytes from the File and stores them in b. It returns
// the number of bytes read and any error encountered. At end of file, Read
// returns 0, io.EOF.
func (f *fileHandle) Read(b []byte) (int, error) {
	f.m.Lock()
	defer f.m.Unlock()
	if f.closed {
		return 0, fmt.Errorf("read %s %w", f.info.name, fs.ErrClosed)
	}

	return f.content.Read(b)
}

// Close closes the File, rendering it unusable for I/O.  Close will return an
// error if it has already been called.
func (f *fileHandle) Close() error {
	f.m.Lock()
	defer f.m.Unlock()

	if f.closed {
		return fmt.Errorf("close %s %w", f.info.name, fs.ErrClosed)
	}
	f.closed = true
	f.content = nil
	return nil
}
