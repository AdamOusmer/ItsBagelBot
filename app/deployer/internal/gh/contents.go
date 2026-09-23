// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/google/go-github/v92/github"
	"golang.org/x/sync/errgroup"

	"ItsBagelBot/app/deployer/internal/ports"
)

func (c *Client) File(ctx context.Context, p ports.FilePath, ref ports.Ref) ([]byte, error) {
	file, _, resp, err := c.gh.Repositories.GetContents(ctx, c.owner, c.repo, string(p),
		&github.RepositoryContentGetOptions{Ref: string(ref)})
	if err != nil {
		return nil, apiErr(resp, err)
	}
	if file == nil {
		return nil, fmt.Errorf("%s at %s is a directory: %w", p, ref, ports.ErrInvalid)
	}
	text, err := file.GetContent()
	return []byte(text), err
}

// Tree lists dir's subtree in one recursive call and reads each blob. The
// tree sha comes from the parent directory's listing so the recursive call
// walks dir alone rather than the whole 3000-file repository.
func (c *Client) Tree(ctx context.Context, dir ports.FilePath, ref ports.Ref) (ports.Files, error) {
	sha, err := c.dirSHA(ctx, dir, ref)
	if err != nil {
		return nil, err
	}
	tree, resp, err := c.gh.Git.GetTree(ctx, c.owner, c.repo, sha, true)
	if err != nil {
		return nil, apiErr(resp, err)
	}
	if tree.GetTruncated() {
		return nil, fmt.Errorf("tree %s at %s truncated by GitHub", dir, ref)
	}
	return c.readBlobs(ctx, dir, tree.GetEntries())
}

func (c *Client) dirSHA(ctx context.Context, dir ports.FilePath, ref ports.Ref) (string, error) {
	parent, name := path.Split(string(dir))
	_, listing, resp, err := c.gh.Repositories.GetContents(ctx, c.owner, c.repo, strings.TrimSuffix(parent, "/"),
		&github.RepositoryContentGetOptions{Ref: string(ref)})
	if err != nil {
		return "", apiErr(resp, err)
	}
	i := slices.IndexFunc(listing, func(e *github.RepositoryContent) bool {
		return e.GetName() == name && e.GetType() == "dir"
	})
	if i < 0 {
		return "", fmt.Errorf("directory %s at %s: %w", dir, ref, ports.ErrNotFound)
	}
	return listing[i].GetSHA(), nil
}

// readBlobs reads the blob entries of a recursive tree listing into files
// keyed by repo-relative path; submodule and tree entries carry no bytes.
func (c *Client) readBlobs(ctx context.Context, dir ports.FilePath, entries []*github.TreeEntry) (ports.Files, error) {
	files := ports.Files{}
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(fanout)
	for _, e := range entries {
		if e.GetType() != "blob" {
			continue
		}
		g.Go(func() error {
			b, err := c.blob(gctx, e.GetSHA())
			mu.Lock()
			defer mu.Unlock()
			files[ports.FilePath(path.Join(string(dir), e.GetPath()))] = b
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return files, nil
}

func (c *Client) blob(ctx context.Context, sha string) ([]byte, error) {
	if b, ok := c.blobs.Load(sha); ok {
		return b.([]byte), nil
	}
	b, resp, err := c.gh.Git.GetBlobRaw(ctx, c.owner, c.repo, sha)
	if err != nil {
		return nil, apiErr(resp, err)
	}
	c.blobs.Store(sha, b)
	return b, nil
}
