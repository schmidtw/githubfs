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

func TestDirHandle_Most(t *testing.T) {
	tests := []struct {
		description string
		name        string
		time        time.Time
		mode        fs.FileMode
	}{
		{
			description: "dir test",
			name:        "foo",
			time:        time.Now(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			assert := assert.New(t)

			fh := dirHandle{
				info: &fileInfo{
					name:    tc.name,
					size:    0,
					modTime: tc.time,
					mode:    tc.mode | fs.ModeDir,
				},
			}

			fi, err := fh.Stat()
			assert.NoError(err)
			assert.Equal(tc.name, fi.Name())
			assert.Equal(int64(0), fi.Size())
			assert.Equal(tc.mode|fs.ModeDir, fi.Mode())
			assert.Equal(tc.time, fi.ModTime())
			assert.True(fi.IsDir())
			assert.Nil(fi.Sys())

			b := make([]byte, 20)
			n, err := fh.Read(b)
			assert.Error(err)
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

func TestDirHandle_ReadDir(t *testing.T) {
	a := &dirEntry{
		info: &fileInfo{
			name: "a",
		},
	}
	b := &dirEntry{
		info: &fileInfo{
			name: "b",
		},
	}
	c := &dirEntry{
		info: &fileInfo{
			name: "c",
		},
	}
	d := &dirEntry{
		info: &fileInfo{
			name: "d",
		},
	}
	tests := []struct {
		description string
		entries     []fs.DirEntry
		closed      bool
		precall     bool
		n           int
		expectedErr error
		expected    []fs.DirEntry
	}{
		{
			description: "simple dir test",
			entries:     []fs.DirEntry{a, b, c, d},
			n:           0,
			expected:    []fs.DirEntry{a, b, c, d},
		}, {
			description: "limited dir test",
			entries:     []fs.DirEntry{a, b, c, d},
			n:           1,
			expected:    []fs.DirEntry{a},
		}, {
			description: "exact dir test",
			entries:     []fs.DirEntry{a},
			n:           1,
			expected:    []fs.DirEntry{a},
		}, {
			description: "large limited dir test",
			entries:     []fs.DirEntry{a, b, c, d},
			n:           7,
			expected:    []fs.DirEntry{a, b, c, d},
			expectedErr: io.EOF,
		}, {
			description: "full dir call after a call",
			entries:     []fs.DirEntry{a, b, c, d},
			n:           0,
			precall:     true,
			expected:    []fs.DirEntry{b, c, d},
		}, {
			description: "partial dir call after a call",
			entries:     []fs.DirEntry{a, b, c, d},
			n:           2,
			precall:     true,
			expected:    []fs.DirEntry{b, c},
		}, {
			description: "large limited dir test after a call",
			entries:     []fs.DirEntry{a, b, c, d},
			n:           7,
			precall:     true,
			expected:    []fs.DirEntry{b, c, d},
			expectedErr: io.EOF,
		}, {
			description: "large limited dir test after a call",
			entries:     []fs.DirEntry{a},
			n:           1,
			precall:     true,
			expected:    []fs.DirEntry{},
			expectedErr: io.EOF,
		}, {
			description: "closed",
			entries:     []fs.DirEntry{a},
			n:           1,
			closed:      true,
			expectedErr: fs.ErrClosed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)
			fh := dirHandle{
				info: &fileInfo{
					name: "testName",
					mode: fs.ModeDir,
				},
				entries: tc.entries,
			}

			if tc.precall {
				_, _ = fh.ReadDir(1)
			}

			if tc.closed {
				_ = fh.Close()
			}

			got, err := fh.ReadDir(tc.n)
			if tc.expectedErr == nil || tc.expectedErr == io.EOF {
				if tc.expectedErr == nil {
					assert.NoError(err)
				} else {
					assert.ErrorIs(err, tc.expectedErr)
				}
				require.Equal(len(tc.expected), len(got))
				for i := range tc.expected {
					assert.Equal(tc.expected[i].Name(), got[i].Name())
				}
				return
			}

			assert.ErrorIs(err, tc.expectedErr)
			assert.Nil(got)
		})
	}
}
