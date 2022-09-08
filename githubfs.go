// SPDX-FileCopyrightText: 2022 Weston Schmidt <weston_schmidt@alumni.purdue.edu>
// SPDX-License-Identifier: Apache-2.0

// Package githubfs is a handy filesystem approach to interacting with assets
// that github provides.
//
// Githubfs provides an easy way to grab a few random files or release objects
// for many repositories at once without needing to interact with the github
// API directly.
//
// The performance for depends on how you are using it.
package githubfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	gql "github.com/hasura/go-graphql-client"
)

// General structure:
// org/
// └── repo
//     ├── git
//     │   └── branch
//     │       └── files
//     ├── packages
//     │   └── container
//     └── releases
//         └── v0.0.1
//             ├── description.md
//             ├── file-0.0.1.tar.gz
//             └── sha256sum.txt
//
//  Depth:
//  0   1    2   3
//  org/repo/git/branch/...
//          /packages/container/...
//          /releases/{tag}/files/...

const (
	dirNameGit      = "git"
	dirNameReleases = "releases"
	dirNamePackages = "packages"
)

const (
	ghModeFile       = 0100644
	ghModeExecutable = 0100755
	ghModeSubmodule  = 0160000
	ghModeSymlink    = 0120000
	ghModeDirectory  = 0040000
)

type input struct {
	org           string
	repo          string
	branch        string
	allowArchived bool
}

// ensure the FS matches the interface
var _ fs.FS = (*FS)(nil)

type FS struct {
	httpClient *http.Client
	gqlClient  *gql.Client
	connected  bool
	githubUrl  string
	inputs     []input
	root       *dir
}

// Option is the type used for options.
type Option func(gfs *FS)

// WithURL provides a way to set the URL for the specific github instance to use.
func WithURL(url string) Option {
	return func(gfs *FS) {
		gfs.githubUrl = url
	}
}

// WithHttpClient provides a way to set the HTTP client to use.
func WithHttpClient(c *http.Client) Option {
	return func(gfs *FS) {
		gfs.httpClient = c
	}
}

// WithFullOrg instructs the filesystem to include all the repositories owned
// by an organization or user and the default branches of each repo.
func WithFullOrg(org string, allowArchivedrepos ...bool) Option {
	with := false
	if len(allowArchivedrepos) > 0 {
		with = allowArchivedrepos[len(allowArchivedrepos)-1]
	}
	return func(gfs *FS) {
		gfs.inputs = append(gfs.inputs, input{org: org, allowArchived: with})
	}
}

// WithRepo configures a specific owner, repository and branch to include.
func WithRepo(org string, repo string, branch ...string) Option {
	var br string
	if len(branch) > 0 {
		br = branch[len(branch)-1]
	}
	return func(gfs *FS) {
		gfs.inputs = append(gfs.inputs, input{
			org:           org,
			repo:          repo,
			branch:        br,
			allowArchived: true,
		})
	}
}

// New creates a new githubfs.FS object with the specified configuration.
func New(opts ...Option) *FS {
	gfs := FS{
		httpClient: http.DefaultClient,
		githubUrl:  "https://api.github.com/graphql",
	}

	for _, opt := range opts {
		opt(&gfs)
	}

	gfs.gqlClient = gql.NewClient(gfs.githubUrl, gfs.httpClient)
	gfs.root = gfs.newDir()
	gfs.root.name = "."

	return &gfs
}

// Open opens the named file.
func (gfs *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  fs.ErrInvalid,
		}
	}

	if err := gfs.connect(); err != nil {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  fmt.Errorf("error connecting: %w", err),
		}
	}

	child, err := gfs.get(name)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  fmt.Errorf("error fetching the file: %w", err),
		}
	}

	switch child := child.(type) {
	case *file:
		if child.size > 0 && child.content == nil {
			if len(child.url) > 0 {
				// TODO Read the file
			}
		}
		return &file{
			name: child.name,
			mode: child.mode,
		}, nil
	case *dir:
		return child.newDirHandle(), nil
	}

	return nil, &fs.PathError{
		Op:   "open",
		Path: name,
		Err:  fmt.Errorf("unexpected file type in fs"),
	}
}

// connect is a helper function that connects to github and figures out the
// repositories that should be included in the file system.
func (gfs *FS) connect() error {
	if gfs.connected {
		return nil
	}

	// Fetch the bulk things first, so specific repos with extra details
	// can be added afterwards safely.
	for _, s := range gfs.inputs {
		if len(s.repo) == 0 {
			if err := gfs.fetchRepos(s); err != nil {
				return err
			}
		}
	}
	for _, s := range gfs.inputs {
		if len(s.repo) != 0 {
			if err := gfs.fetchRepo(s); err != nil {
				return err
			}
		}
	}
	gfs.connected = true
	return nil
}

// get fetches a directory or file by it's path.
func (gfs *FS) get(path string) (any, error) {
	if path == "." {
		return gfs.root, nil
	}

	parts := strings.Split(path, "/")

	cur := gfs.root
	for i, part := range parts {
		child := cur.children[part]
		if child == nil {
			return nil, fmt.Errorf("not a directory: %s: %w", part, fs.ErrNotExist)
		}
		if _, isFile := child.(*file); isFile {
			if i == len(parts)-1 {
				return child, nil
			} else {
				return nil, fmt.Errorf("no such file or directory: %s: %w", part, fs.ErrNotExist)
			}
		}
		tmp, ok := child.(*dir)
		if !ok {
			return nil, errors.New("not a directory")
		}
		cur = tmp
		if !cur.isFetched {
			if i >= 2 {
				switch parts[2] {
				case dirNameGit:
					if len(parts) >= 4 && i >= 3 {
						if err := gfs.getGitDir(cur, parts[4:i+1]); err != nil {
							return nil, err
						}
					}
				case dirNamePackages:
					// TODO
				case dirNameReleases:
					if err := gfs.getReleaseDir(cur, parts[3:i+1]); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	return cur, nil
}

// newDir creates a new directory object tied to the githubfs root.
func (gfs *FS) newDir() *dir {
	return &dir{
		gfs:      gfs,
		perm:     fs.ModeDir,
		children: make(map[string]any),
	}
}

// findOrAdd finds existing node or adds it.
func (gfs *FS) findOrAdd(tree map[string]any, name, org, repo, branch string) *dir {
	if tmp, found := tree[name]; found {
		return tmp.(*dir)
	}

	d := gfs.newDir()
	d.org = org
	d.repo = repo
	d.branch = branch
	d.perm = fs.ModeDir
	d.name = name
	tree[name] = d

	return d
}

// newRepo creates a new repo structure if it isn't present already.  Each needed
// node is created and linked.  The resulting nodes are returned by a map.
func (gfs *FS) newRepo(org, repo, branch string, releases, packages bool, size int) (out map[string]*dir) {
	out = make(map[string]*dir)

	o := gfs.findOrAdd(gfs.root.children, org, org, "", "")
	r := gfs.findOrAdd(o.children, repo, org, repo, "")
	r.size = size
	if releases {
		out["rel"] = gfs.findOrAdd(r.children, dirNameReleases, org, repo, "")
	}
	if packages {
		out["pkg"] = gfs.findOrAdd(r.children, dirNamePackages, org, repo, "")
	}

	git := gfs.findOrAdd(r.children, dirNameGit, org, repo, "")
	git.size = size
	if len(branch) > 0 {
		out["branch"] = gfs.findOrAdd(git.children, branch, org, repo, branch)
		out["branch"].size = size
	}

	out["org"] = o
	out["repo"] = r
	out["git"] = git

	return out
}

// fetchRepo calls github and asks for a single specific repo, and links it
// back to the filesystem.
func (gfs *FS) fetchRepo(s input) (err error) {
	vars := map[string]any{
		"owner": s.org,
		"repo":  s.repo,
	}

	var query struct {
		Repo struct {
			DiskUsage        int
			IsArchived       bool
			IsDisabled       bool
			NameWithOwner    string
			DefaultBranchRef struct {
				Name string
			}
			Releases struct {
				TotalCount int
			}
		} `graphql:"repository(name: $repo, owner: $owner)"`
	}

	if err = gfs.gqlClient.Query(context.Background(), &query, vars); err != nil {
		return err
	}

	if !s.allowArchived && query.Repo.IsArchived ||
		query.Repo.IsDisabled ||
		query.Repo.NameWithOwner != s.org+"/"+s.repo {
		return nil
	}

	branch := s.branch
	if len(branch) == 0 {
		branch = query.Repo.DefaultBranchRef.Name
	}
	releases := query.Repo.Releases.TotalCount > 0
	_ = gfs.newRepo(s.org, s.repo, branch, releases, false, query.Repo.DiskUsage)

	return nil
}

// fetchRepos calls githun and asks for all the repos in an org/user space, and
// links them back to the filesystem.
func (gfs *FS) fetchRepos(s input) (err error) {
	vars := map[string]any{
		"owner": s.org,
		"count": 100,
		"after": (*string)(nil),
	}

	more := true
	for more {
		var query struct {
			Owner struct {
				Repo struct {
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
					Edges []struct {
						Node struct {
							Name             string
							DiskUsage        int
							IsArchived       bool
							IsDisabled       bool
							NameWithOwner    string
							DefaultBranchRef struct {
								Name string
							}
							Releases struct {
								TotalCount int
							}
						}
					}
				} `graphql:"repositories(orderBy: {field: NAME, direction: ASC}, first: $count, after: $after)"`
			} `graphql:"repositoryOwner(login: $owner)"`
		}

		if err = gfs.gqlClient.Query(context.Background(), &query, vars); err != nil {
			return err
		}

		for _, edge := range query.Owner.Repo.Edges {
			if !s.allowArchived && edge.Node.IsArchived ||
				edge.Node.IsDisabled ||
				edge.Node.NameWithOwner != s.org+"/"+edge.Node.Name {
				continue
			}

			branch := edge.Node.DefaultBranchRef.Name
			releases := edge.Node.Releases.TotalCount > 0
			size := edge.Node.DiskUsage
			_ = gfs.newRepo(s.org, edge.Node.Name, branch, releases, false, size)
		}

		more = query.Owner.Repo.PageInfo.HasNextPage
		vars["after"] = query.Owner.Repo.PageInfo.EndCursor
	}

	return nil
}

// getEntireGitDir fetches the entire directory as a tarball and decodes the
// result into the filesystem subtree.  For small repos this is much faster.
func (gfs *FS) getEntireGitDir(d *dir, parts []string) error {
	/*
	   query {
	     repository(name: "talaria", owner: "xmidt-org") {
	       ref(qualifiedName: "refs/heads/main") {
	         target {
	           ... on Commit {
	             tarballUrl
	             repository {
	               diskUsage
	             }
	           }
	         }
	       }
	     }
	   }
	*/
	// TODO
	return nil
}

// getGitDir fetches a single directory via the github API. This isn't fast, but
// there are conditions where it is adventageous over fetching everything all at
// once.
func (gfs *FS) getGitDir(d *dir, parts []string) error {
	vars := map[string]any{
		"owner": d.org,
		"repo":  d.repo,
		"exp":   d.branch + ":" + strings.Join(parts, "/"),
	}

	/*
		query {
		  repository(name: ".github", owner: "xmidt-org", followRenames: false) {
		    object(expression: "main:") {
		      ... on Tree {
		        entries {
		          name
		          path
		          size
		          type
		          oid
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
						Size int
						Mode int
					}
				} `graphql:"... on Tree"`
			} `graphql:"object(expression: $exp)"`
		} `graphql:"repository(name: $repo, owner: $owner, followRenames: true)"`
	}

	if err := gfs.gqlClient.Query(context.Background(), &query, vars); err != nil {
		return err
	}

	for _, entry := range query.Repository.Object.Tree.Entries {
		switch entry.Mode {
		case ghModeFile:
			f := d.newFile(entry.Name, entry.Size, 0644)
			d.children[f.name] = f
		case ghModeExecutable:
			f := d.newFile(entry.Name, entry.Size, 0755)
			d.children[f.name] = f
		case ghModeDirectory:
			dir := d.newDir(entry.Name)
			d.children[dir.name] = dir
		case ghModeSubmodule:
			// TODO
		case ghModeSymlink:
			// TODO
		default:
			panic(entry.Mode)
		}
	}

	return nil
}

// getReleaseDir fetches the release information and makes it into a directory
// structure that is linked to the filesystem
func (gfs *FS) getReleaseDir(d *dir, parts []string) error {
	vars := map[string]any{
		"owner": d.org,
		"repo":  d.repo,
		"count": 100,
		"after": (*string)(nil),
	}

	/*	query MyQuery {
		  repository(name: "repo", owner: "org") {
		    releases(first: 10, orderBy: {field: CREATED_AT, direction: DESC}) {
		      edges {
		        node {
		          tag {
		            name
		          }
		          isPrerelease
		          isDraft
		          isLatest
		             createdAt
		          name
		          description
		          releaseAssets(first: 10) {
		            edges {
		              node {
		                downloadUrl
		                name
		                size
		              }
		            }
		          }
		        }
		      }
		    }
		  }
		}
	*/
	more := true
	for more {
		var query struct {
			Repository struct {
				Releases struct {
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
					Edges []struct {
						Node struct {
							Tag struct {
								Name string
							}
							IsPrerelease  bool
							IsDraft       bool
							CreatedAt     string
							Description   string
							ReleaseAssets struct {
								Edges []struct {
									Node struct {
										DownloadUrl string
										Name        string
										Size        int
									}
								}
							} `graphql:"releaseAssets(first:100)"`
						}
					}
				} `graphql:"releases(first: $count, orderBy: {field: CREATED_AT, direction: DESC}, after: $after)"`
			} `graphql:"repository(name: $repo, owner: $owner)"`
		}

		if err := gfs.gqlClient.Query(context.Background(), &query, vars); err != nil {
			return err
		}

		for _, edge := range query.Repository.Releases.Edges {
			if edge.Node.IsDraft || edge.Node.IsPrerelease {
				continue
			}

			tag := edge.Node.Tag.Name
			desc := edge.Node.Description

			relDir := d.newDir(tag)
			relDir.perm |= 0755
			d.children[tag] = relDir

			descFile := relDir.newFile("description.md", len(desc), 0644)
			descFile.content = bytes.NewBufferString(desc)
			relDir.children["description.md"] = descFile

			for _, asset := range edge.Node.ReleaseAssets.Edges {
				f := relDir.newFile(asset.Node.Name, asset.Node.Size, 0644)
				f.url = asset.Node.DownloadUrl
				relDir.children[asset.Node.Name] = f
			}
			relDir.fetched()
		}

		more = query.Repository.Releases.PageInfo.HasNextPage
		vars["after"] = query.Repository.Releases.PageInfo.EndCursor
	}

	return nil
}

// dir represents a directory node.
type dir struct {
	m         sync.Mutex
	gfs       *FS
	org       string
	repo      string
	name      string
	branch    string
	size      int
	perm      os.FileMode
	modTime   time.Time
	children  map[string]any
	isFetched bool
	entries   []fs.DirEntry
}

// newDir creates a new directory node tied to the existing node.
func (d *dir) newDir(name string) *dir {
	n := d.gfs.newDir()
	n.org = d.org
	n.repo = d.repo
	n.name = name
	n.branch = d.branch
	n.perm = 0755

	return n
}

// newDirHandle creates a new dirHandle and returns it.
func (d *dir) newDirHandle() *dirHandle {
	d.fetched()
	return &dirHandle{
		dir: d,
	}
}

// fetched does the work to indicate the structure is fetched and reading
// the directory is now possible.
func (d *dir) fetched() {
	d.m.Lock()
	defer d.m.Unlock()

	if d.isFetched {
		return
	}

	d.isFetched = true

	d.entries = make([]fs.DirEntry, 0, len(d.children))

	for _, child := range d.children {
		switch child := child.(type) {
		case *file:
			stat, _ := child.Stat()
			d.entries = append(d.entries, &dirEntry{
				info: stat,
			})
		case *dir:
			fi := fileInfo{
				name:    child.name,
				size:    4096,
				modTime: child.modTime,
				mode:    child.perm | fs.ModeDir,
			}
			d.entries = append(d.entries, &dirEntry{
				info: &fi,
			})
		}
	}
}

// newFile creates a new file object based on a specific dir object.
func (d *dir) newFile(name string, size int, mode int) *file {
	return &file{
		gfs:    d.gfs,
		owner:  d.org,
		repo:   d.repo,
		branch: d.branch,
		mode:   fs.FileMode(mode),
		name:   name,
		size:   int64(size),
	}
}

// ensure the dirHandle matches the interface.
var _ fs.ReadDirFile = (*dirHandle)(nil)

// dirHandle provides a directory with an index for supporting the ReadDir
// operation.  The index shows where in the list of files in the directory
// a walk operation is, so this can't be merged with the dir object.
type dirHandle struct {
	m     sync.Mutex
	dir   *dir
	index int
}

// Stat returns a FileInfo describing the file.
func (d *dirHandle) Stat() (fs.FileInfo, error) {
	return &fileInfo{
		name:    d.dir.name,
		size:    4096,
		modTime: d.dir.modTime,
		mode:    d.dir.perm | fs.ModeDir,
	}, nil
}

// Read fulfills the fs.File requirement.
func (d *dirHandle) Read(b []byte) (int, error) {
	return 0, fmt.Errorf("is a directory not a file")
}

// Close fulfills the fs.File requirement.
func (d *dirHandle) Close() error {
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
	d.dir.m.Lock()
	defer d.dir.m.Unlock()

	l := len(d.dir.entries)

	var rv []fs.DirEntry

	if n <= 0 {
		rv := d.dir.entries[d.index:]
		d.index = l
		return rv, nil
	}

	n = d.index + n
	if l < n {
		n = l
	}

	rv = d.dir.entries[d.index:n]
	d.index += n

	if d.index == l {
		return rv, io.EOF
	}

	if 0 == l || d.index == l {
		return nil, io.EOF
	}

	return rv, nil
}

// ensure the File matches the interface
var _ fs.File = (*file)(nil)

// file provides the concrete fs.File implementation for the filesystem.
type file struct {
	m       sync.Mutex
	gfs     *FS
	owner   string
	repo    string
	branch  string
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	url     string
	content *bytes.Buffer
	closed  bool
}

// Stat returns a FileInfo describing the file.
func (f *file) Stat() (fs.FileInfo, error) {
	f.m.Lock()
	defer f.m.Unlock()
	if f.closed {
		return nil, &fs.PathError{
			Op:   "stat",
			Path: f.name,
			Err:  fs.ErrClosed,
		}
	}

	fi := fileInfo{
		name:    f.name,
		size:    f.size,
		modTime: f.modTime,
		mode:    f.mode,
	}
	return &fi, nil
}

// Read reads the named file and returns the contents back in the byte array
// passed in.
func (f *file) Read(b []byte) (int, error) {
	f.m.Lock()
	defer f.m.Unlock()
	if f.closed {
		return 0, &fs.PathError{
			Op:   "read",
			Path: f.name,
			Err:  fs.ErrClosed,
		}
	}
	n, err := f.content.Read(b)
	if err != nil {
		return 0, &fs.PathError{
			Op:   "read",
			Path: f.name,
			Err:  err,
		}
	}

	return n, nil
}

// Close closes the file.
func (f *file) Close() error {
	f.m.Lock()
	defer f.m.Unlock()
	if f.closed {
		return &fs.PathError{
			Op:   "close",
			Path: f.name,
			Err:  fs.ErrClosed,
		}
	}
	f.closed = true
	return nil
}

// ensure the fileInfo matches the interface.
var _ fs.FileInfo = (*fileInfo)(nil)

// fileInfo describes a file and is returned by Stat.
type fileInfo struct {
	name    string
	size    int64
	modTime time.Time
	mode    fs.FileMode
}

// Name returns the base name of the file.
func (fi *fileInfo) Name() string {
	return fi.name
}

// Size returns the length in bytes for regular files; system-dependent for others.
func (fi *fileInfo) Size() int64 {
	return fi.size
}

// Mode returns the file mode bits.
func (fi *fileInfo) Mode() fs.FileMode {
	return fi.mode
}

// ModTime returns the modification time.
func (fi *fileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir returns the abbreviation for Mode().IsDir().
func (fi *fileInfo) IsDir() bool {
	return fi.mode&fs.ModeDir > 0
}

// Sys returns the underlying data source (can return nil).  (Always nil).
func (fi *fileInfo) Sys() any {
	return nil
}

// ensure the dirEntry matches the interface.
var _ fs.DirEntry = (*dirEntry)(nil)

type dirEntry struct {
	info fs.FileInfo
}

// Name returns the name of the file (or subdirectory) described by the entry.
// This name is only the final element of the path (the base name), not the entire path.
// For example, Name would return "hello.go" not "home/gopher/hello.go".
func (d *dirEntry) Name() string {
	return d.info.Name()
}

// IsDir reports whether the entry describes a directory.
func (d *dirEntry) IsDir() bool {
	return d.info.IsDir()
}

// Type returns the type bits for the entry.
// The type bits are a subset of the usual FileMode bits, those returned by the FileMode.Type method.
func (d *dirEntry) Type() fs.FileMode {
	return d.info.Mode()
}

// Info returns the FileInfo for the file or subdirectory described by the entry.
// The returned FileInfo may be from the time of the original directory read
// or from the time of the call to Info. If the file has been removed or renamed
// since the directory read, Info may return an error satisfying errors.Is(err, ErrNotExist).
// If the entry denotes a symbolic link, Info reports the information about the link itself,
// not the link's target.
func (d *dirEntry) Info() (fs.FileInfo, error) {
	// TODO sym link support
	return d.info, nil
}
