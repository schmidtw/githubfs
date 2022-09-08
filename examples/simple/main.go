// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"

	"github.com/schmidtw/githubfs"
	"golang.org/x/oauth2"
)

func main() {
	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GITHUB_TOKEN")},
	)
	httpClient := oauth2.NewClient(context.Background(), src)

	gfs := githubfs.New(
		githubfs.WithHttpClient(httpClient),
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
}
