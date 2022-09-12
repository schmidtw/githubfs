// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package githubfs

import (
	"io"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileHandle_All(t *testing.T) {
	tests := []struct {
		description string
		name        string
		size        int64
		time        time.Time
		mode        fs.FileMode
		isDir       bool
		content     []byte
	}{
		{
			description: "file test",
			name:        "foo",
			size:        1234,
			time:        time.Now(),
			isDir:       false,
			content:     []byte("hello, world"),
		}, {
			description: "empty file test",
			name:        "empty",
			size:        0,
			time:        time.Now(),
			isDir:       true,
			mode:        fs.ModeDir,
			content:     []byte{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			info := fileInfo{
				name:    tc.name,
				size:    tc.size,
				modTime: tc.time,
				mode:    tc.mode,
			}
			fh := newFileHandle(info, tc.content)
			require.NotNil(fh)

			fi, err := fh.Stat()
			assert.NoError(err)
			assert.Equal(tc.name, fi.Name())
			assert.Equal(tc.size, fi.Size())
			assert.Equal(tc.mode, fi.Mode())
			assert.Equal(tc.time, fi.ModTime())
			assert.Equal(tc.isDir, fi.IsDir())
			assert.Nil(fi.Sys())

			b := make([]byte, 20)
			n, err := fh.Read(b)

			if len(tc.content) != 0 {
				assert.NoError(err)
			} else {
				assert.ErrorIs(err, io.EOF)
			}
			assert.Equal(len(tc.content), n)
			assert.Equal(tc.content, b[:n])

			n, err = fh.Read(b)
			assert.ErrorIs(io.EOF, err)
			assert.Equal(0, n)

			err = fh.Close()
			assert.NoError(err)

			err = fh.Close()
			assert.ErrorIs(err, fs.ErrClosed)

			n, err = fh.Read(b)
			assert.ErrorIs(err, fs.ErrClosed)
			assert.Equal(0, n)

			fi, err = fh.Stat()
			assert.ErrorIs(err, fs.ErrClosed)
			assert.Nil(fi)
		})
	}
}
