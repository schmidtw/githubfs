// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0
package githubfs

import (
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDirEntry_All(t *testing.T) {
	tests := []struct {
		description string
		name        string
		size        int64
		time        time.Time
		mode        fs.FileMode
		isDir       bool
	}{
		{
			description: "file test",
			name:        "foo",
			size:        1234,
			time:        time.Now(),
			isDir:       false,
		}, {
			description: "dir test",
			name:        "dir",
			size:        0,
			time:        time.Now(),
			isDir:       true,
			mode:        fs.ModeDir,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			assert := assert.New(t)

			de := dirEntry{
				info: &fileInfo{
					name:    tc.name,
					size:    tc.size,
					modTime: tc.time,
					mode:    tc.mode,
				},
			}

			assert.Equal(tc.name, de.Name())
			assert.Equal(tc.mode, de.Type())
			assert.Equal(tc.isDir, de.IsDir())

			info, err := de.Info()
			assert.NoError(err)

			assert.Equal(tc.name, info.Name())
			assert.Equal(tc.size, info.Size())
			assert.Equal(tc.time, info.ModTime())
			assert.Equal(tc.mode, info.Mode())
			assert.Equal(tc.isDir, info.IsDir())
			assert.Nil(info.Sys())
		})
	}
}
