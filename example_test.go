// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package githubfs_test

import (
	"context"
	"fmt"
	"io/fs"
	"os"

	"github.com/schmidtw/githubfs"
	"golang.org/x/oauth2"
)

func Example() {
	token := os.Getenv("GITHUB_TOKEN")

	// Github requires credentials to use the v4 API.  Bypass this in the
	// tests to prevent false failures, but enable folks to easily try out
	// the feature.
	if len(token) == 0 {
		fmt.Println("schmidtw/githubfs/git/main/.reuse")
		fmt.Println("schmidtw/githubfs/git/main/.reuse/dep5")
		return
	}

	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	httpClient := oauth2.NewClient(context.Background(), src)

	gfs := githubfs.New(
		githubfs.WithHttpClient(httpClient),
		githubfs.WithRepo("schmidtw", "githubfs"),
	)

	err := fs.WalkDir(gfs, "schmidtw/githubfs/git/main/.reuse",
		func(path string, d fs.DirEntry, err error) error {
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
	// schmidtw/githubfs/git/main/.reuse
	// schmidtw/githubfs/git/main/.reuse/dep5
}
