// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
)

const (
	byTagPath  = "GET /repos/o/r/releases/tags/v0.3.0-beta"
	latestPath = "GET /repos/o/r/releases/latest"
	release3   = `{"id":3,"tag_name":"v0.3.0-beta","name":"v0.3.0-beta","html_url":"r3",
		"target_commitish":"c1","published_at":"2026-09-23T10:00:00Z"}`
)

var published = time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

type releaseOutcome struct {
	Release ports.Release
	Found   bool
	Kind    error
}

func TestRelease(t *testing.T) {
	cases := []struct {
		name string
		rt   routes
		want releaseOutcome
	}{
		{
			name: "latest release",
			rt:   routes{byTagPath: reply(http.StatusOK, release3), latestPath: reply(http.StatusOK, `{"id":3}`)},
			want: releaseOutcome{Found: true, Release: ports.Release{
				ID: 3, Tag: "v0.3.0-beta", Title: "v0.3.0-beta", URL: "r3", TargetCommitish: "c1", Latest: true, PublishedAt: published,
			}},
		},
		{
			name: "older release",
			rt:   routes{byTagPath: reply(http.StatusOK, release3), latestPath: reply(http.StatusOK, `{"id":4}`)},
			want: releaseOutcome{Found: true, Release: ports.Release{
				ID: 3, Tag: "v0.3.0-beta", Title: "v0.3.0-beta", URL: "r3", TargetCommitish: "c1", PublishedAt: published,
			}},
		},
		{
			name: "no release for the tag",
			rt:   routes{byTagPath: reply(http.StatusNotFound, `{"message":"Not Found"}`)},
			want: releaseOutcome{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := newClient(t, tc.rt)
			rel, ok, err := c.Release(t.Context(), "v0.3.0-beta")
			assert.Equal(t, tc.want, releaseOutcome{Release: rel, Found: ok, Kind: kindOf(err)})
		})
	}
}

type upsertOutcome struct {
	Writes []string
	Body   string
}

func TestUpsertRelease(t *testing.T) {
	const (
		create = "POST /repos/o/r/releases"
		update = "PATCH /repos/o/r/releases/3"
	)
	cases := []struct {
		name  string
		byTag http.HandlerFunc
		want  upsertOutcome
	}{
		{
			name:  "creates",
			byTag: reply(http.StatusNotFound, `{"message":"Not Found"}`),
			want: upsertOutcome{Writes: []string{create}, Body: `{"tag_name":"v0.3.0-beta","target_commitish":"c2",
				"name":"v0.3.0-beta","body":"notes","prerelease":false,"make_latest":"true"}`},
		},
		{
			name:  "hotfix re-points the existing release",
			byTag: reply(http.StatusOK, release3),
			want: upsertOutcome{Writes: []string{update}, Body: `{"target_commitish":"c2",
				"name":"v0.3.0-beta","body":"notes","prerelease":false,"make_latest":"true"}`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, fake, _ := newClient(t, routes{
				byTagPath: tc.byTag,
				create:    reply(http.StatusCreated, release3),
				update:    reply(http.StatusOK, release3),
			})
			rel, err := c.UpsertRelease(t.Context(), ports.ReleaseSpec{Tag: "v0.3.0-beta", Title: "v0.3.0-beta", Notes: "notes", Target: "c2"})
			require.NoError(t, err)
			writes := fake.writes()
			assert.Equal(t, []any{tc.want.Writes, true}, []any{writes, rel.Latest})
			assert.JSONEq(t, tc.want.Body, fake.bodies[writes[0]])
		})
	}
}

func TestReleases(t *testing.T) {
	c, fake, _ := newClient(t, routes{
		"GET /repos/o/r/releases": reply(http.StatusOK, `[
			{"id":5,"tag_name":"v0.5.0-beta","draft":true},
			{"id":4,"tag_name":"v0.4.0-beta"},
			{"id":3,"tag_name":"v0.3.0-beta"}]`),
		latestPath: reply(http.StatusOK, `{"id":4}`),
	})
	rels, err := c.Releases(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, []any{
		[]ports.Release{{ID: 4, Tag: "v0.4.0-beta", Latest: true}, {ID: 3, Tag: "v0.3.0-beta"}},
		"GET /repos/o/r/releases?per_page=10",
	}, []any{rels, fake.calls[0]})
}
