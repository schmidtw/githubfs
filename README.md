# githubfs

[![CI](https://github.com/schmidtw/githubfs/actions/workflows/ci.yml/badge.svg)](https://github.com/schmidtw/githubfs/actions/workflows/ci.yml)
[![codecov.io](http://codecov.io/github/schmidtw/githubfs/coverage.svg?branch=main)](http://codecov.io/github/schmidtw/githubfs?branch=main)
[![Go Report Card](https://goreportcard.com/badge/github.com/schmidtw/githubfs)](https://goreportcard.com/report/github.com/schmidtw/githubfs)
[![GitHub Release](https://img.shields.io/github/release/schmidtw/githubfs.svg)](CHANGELOG.md)
[![GoDoc](https://pkg.go.dev/badge/github.com/schmidtw/githubfs)](https://pkg.go.dev/github.com/schmidtw/githubfs)

A simple to use go fs.FS based github filesystem.

## Resulting Directory Structure

```
org_or_user/                    // org or username
└── repository                  // repository
    ├── git                     // fixed name 'git'
    │   └── main                // the branch name
    │       └── README.md       // the files in the repo
    └── releases                // fixed name 'releases'
        └── v0.0.1              // release version
            └── description.md  // the description of the release and other files from the release
```

## Example Usage

```golang
package githubfs

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
}
```

## Limitations

- Symlinks are only supported for files fetched for small repos (where the fetch
  occurs via a tarball).
- Packages are not supported by the github graphql API, so they aren't supported here.
- Gists are not supported presently.
