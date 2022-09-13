// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

package githubfs

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
)

// getGitDirV3_3 fetches a single directory via the github API. This isn't fast,
// but there are conditions where it is advantageous over fetching everything
// all at  once.
//
// Github Enterprise v3.3 doesn't support size.
func getGitDirV3_3(gfs *FS, d *dir) error {
	path := strings.Join(d.path, "/")

	vars := map[string]any{
		"owner": d.org,
		"repo":  d.repo,
		"exp":   d.branch + ":" + path,
	}

	/*
		query {
		  repository(name: "repo", owner: "org") {
		    object(expression: "main:") {
		      ... on Tree {
		        entries {
		          name
		          mode
		        }
		      }
		    }
		  }
		}
	*/
	var query struct {
		Repository struct {
			Object struct {
				Tree struct {
					Entries []struct {
						Name string
						Mode int
					}
				} `graphql:"... on Tree"`
			} `graphql:"object(expression: $exp)"`
		} `graphql:"repository(name: $repo, owner: $owner)"`
	}

	if err := gfs.gqlClient.Query(context.Background(), &query, vars); err != nil {
		return err
	}

	for _, entry := range query.Repository.Object.Tree.Entries {
		url := strings.Join([]string{gfs.rawUrl, d.org, d.repo, d.branch, path, entry.Name}, "/")

		switch entry.Mode {
		case ghModeFile:
			d.addFile(entry.Name, withUrl(url))
		case ghModeExecutable:
			d.addFile(entry.Name, withUrl(url), withMode(fs.FileMode(0755)))
		case ghModeDirectory:
			d.newDir(entry.Name, withFetcher(getGitDirV3_3))
		case ghModeSubmodule: // TODO
		case ghModeSymlink: // TODO
		default:
			return fmt.Errorf("unknown file mode")
		}
	}

	return nil
}
