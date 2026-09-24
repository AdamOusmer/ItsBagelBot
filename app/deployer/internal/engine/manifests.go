// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"time"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// tagsTTL bounds how long a plan trusts a registry tag listing: main's build
// pushes its tags minutes after the commit lands.
const tagsTTL = time.Minute

type manifestCache struct {
	mu   sync.Mutex
	sha  deploy.SHA
	objs ports.Objects
}

func (c *manifestCache) get(sha deploy.SHA) (ports.Objects, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.objs, c.objs != nil && c.sha == sha
}

func (c *manifestCache) put(sha deploy.SHA, objs ports.Objects) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sha, c.objs = sha, objs
}

type tagEntry struct {
	at   time.Time
	tags []deploy.Tag
}

type tagCache struct {
	mu      sync.Mutex
	entries map[deploy.ImageName]tagEntry
}

func (c *tagCache) get(img deploy.ImageName, now time.Time) ([]deploy.Tag, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[img]
	return e.tags, ok && now.Sub(e.at) < tagsTTL
}

func (c *tagCache) put(img deploy.ImageName, now time.Time, tags []deploy.Tag) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[deploy.ImageName]tagEntry{}
	}
	c.entries[img] = tagEntry{at: now, tags: tags}
}

// mainObjects builds main's manifests at sha. A commit's files never change,
// so a repeat plan on the same head skips the GitHub tree fetch (42 files,
// one API call each), which dominated every plan and every kind switch.
func (e *Engine) mainObjects(ctx context.Context, sha deploy.SHA) (ports.Objects, error) {
	if objs, ok := e.manifests.get(sha); ok {
		return objs, nil
	}
	d := e.d.Stage
	files, err := d.GitHub.Tree(ctx, d.Config.ManifestDir, ports.Ref(sha))
	if err != nil {
		return nil, err
	}
	objs, err := d.Applier.Build(ctx, ports.BuildSpec{Files: files, Root: d.Config.ManifestDir})
	if err != nil {
		return nil, err
	}
	e.manifests.put(sha, objs)
	return objs, nil
}

func (e *Engine) imageTags(ctx context.Context, img deploy.ImageName) ([]deploy.Tag, error) {
	now := e.d.Stage.Clock.Now()
	if tags, ok := e.tags.get(img, now); ok {
		return tags, nil
	}
	tags, err := e.d.Stage.Registry.Tags(ctx, img)
	if err == nil {
		e.tags.put(img, now, tags)
	}
	return tags, err
}
