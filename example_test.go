// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package githubfs_test

import (
	"fmt"
	"io/fs"

	"github.com/schmidtw/githubfs"
)

func ExampleTest() {
	gfs := githubfs.New(
		githubfs.WithRepo("schmidtw", "githubfs"),
	)

	err := fs.WalkDir(gfs, ".", func(path string, d fs.DirEntry, err error) error {
		fmt.Printf("%s\n", path)
		if err != nil || d.IsDir() {
			return err
		}

		return nil
	})
	if err != nil {
		panic(err)
	}

	// Output:
	// foo
}
