// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"net/http"
	"testing"

	"github.com/google/go-github/v92/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const (
	tagRefPath   = "GET /repos/o/r/git/ref/tags/v0.3.0-beta"
	tagObjPath   = "GET /repos/o/r/git/tags/t1"
	createTag    = "POST /repos/o/r/git/tags"
	createRef    = "POST /repos/o/r/git/refs"
	moveTagRef   = "PATCH /repos/o/r/git/refs/tags/v0.3.0-beta"
	annotatedRef = `{"ref":"refs/tags/v0.3.0-beta","object":{"sha":"t1","type":"tag"}}`
)

var tagObject = reply(http.StatusOK, `{"sha":"t1","object":{"sha":"c1","type":"commit"}}`)

// TestLatestTag pins semver order: v0.10.0-beta sorts before v0.9.0-beta as
// a string, and tags outside the vX.Y.Z-beta shape never count.
func TestLatestTag(t *testing.T) {
	c, _, _ := newClient(t, routes{
		"GET /repos/o/r/git/matching-refs/tags/v": reply(http.StatusOK, `[
			{"ref":"refs/tags/v0.9.0-beta","object":{"sha":"c9","type":"commit"}},
			{"ref":"refs/tags/v0.10.0-beta","object":{"sha":"t1","type":"tag"}},
			{"ref":"refs/tags/v1.0.0-rc1","object":{"sha":"cr","type":"commit"}},
			{"ref":"refs/tags/v0.2.0-beta","object":{"sha":"c2","type":"commit"}}]`),
		tagObjPath: tagObject,
	})
	got, err := c.LatestTag(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ports.TagRef{Name: "v0.10.0-beta", CommitSHA: "c1", ObjectSHA: "t1"}, got)
}

func TestLatestTagNone(t *testing.T) {
	c, _, _ := newClient(t, routes{
		"GET /repos/o/r/git/matching-refs/tags/v": reply(http.StatusOK, `[{"ref":"refs/tags/v1","object":{"sha":"c","type":"commit"}}]`),
	})
	_, err := c.LatestTag(t.Context())
	assert.ErrorIs(t, err, ports.ErrNotFound)
}

type tagOutcome struct {
	Ref    ports.TagRef
	Found  bool
	Writes []string
	Kind   error
}

func TestTag(t *testing.T) {
	cases := []struct {
		name string
		rt   routes
		want tagOutcome
	}{
		{
			name: "annotated tag peels to its commit",
			rt:   routes{tagRefPath: reply(http.StatusOK, annotatedRef), tagObjPath: tagObject},
			want: tagOutcome{Ref: ports.TagRef{Name: "v0.3.0-beta", CommitSHA: "c1", ObjectSHA: "t1"}, Found: true},
		},
		{
			name: "lightweight tag is its commit",
			rt: routes{tagRefPath: reply(http.StatusOK,
				`{"ref":"refs/tags/v0.3.0-beta","object":{"sha":"c1","type":"commit"}}`)},
			want: tagOutcome{Ref: ports.TagRef{Name: "v0.3.0-beta", CommitSHA: "c1", ObjectSHA: "c1"}, Found: true},
		},
		{
			name: "missing tag is not an error",
			rt:   routes{tagRefPath: reply(http.StatusNotFound, `{"message":"Not Found"}`)},
			want: tagOutcome{},
		},
		{
			name: "server error surfaces",
			rt:   routes{tagRefPath: reply(http.StatusBadGateway, `{"message":"bad gateway"}`)},
			want: tagOutcome{Kind: errOther},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := newClient(t, tc.rt)
			ref, ok, err := c.Tag(t.Context(), "v0.3.0-beta")
			assert.Equal(t, tc.want, tagOutcome{Ref: ref, Found: ok, Kind: kindOf(err)})
		})
	}
}

func TestUpsertTag(t *testing.T) {
	missing := reply(http.StatusNotFound, `{"message":"Not Found"}`)
	newObject := reply(http.StatusCreated, `{"sha":"t2","object":{"sha":"c2","type":"commit"}}`)
	cases := []struct {
		name string
		spec ports.TagSpec
		rt   routes
		want tagOutcome
	}{
		{
			name: "tag already on the commit is left alone",
			spec: ports.TagSpec{Name: "v0.3.0-beta", Commit: "c1"},
			rt:   routes{tagRefPath: reply(http.StatusOK, annotatedRef), tagObjPath: tagObject},
			want: tagOutcome{Ref: ports.TagRef{Name: "v0.3.0-beta", CommitSHA: "c1", ObjectSHA: "t1"}},
		},
		{
			name: "tag elsewhere without force conflicts",
			spec: ports.TagSpec{Name: "v0.3.0-beta", Commit: "c2"},
			rt:   routes{tagRefPath: reply(http.StatusOK, annotatedRef), tagObjPath: tagObject},
			want: tagOutcome{Kind: ports.ErrConflict},
		},
		{
			name: "hotfix force-moves the ref to a new tag object",
			spec: ports.TagSpec{Name: "v0.3.0-beta", Commit: "c2", Force: true},
			rt: routes{
				tagRefPath: reply(http.StatusOK, annotatedRef), tagObjPath: tagObject,
				createTag: newObject, moveTagRef: reply(http.StatusOK, `{}`),
			},
			want: tagOutcome{
				Ref:    ports.TagRef{Name: "v0.3.0-beta", CommitSHA: "c2", ObjectSHA: "t2"},
				Writes: []string{createTag, moveTagRef},
			},
		},
		{
			name: "new tag creates object then ref",
			spec: ports.TagSpec{Name: "v0.3.0-beta", Commit: "c2"},
			rt: routes{
				tagRefPath: missing, createTag: newObject, createRef: reply(http.StatusCreated, `{}`),
			},
			want: tagOutcome{
				Ref:    ports.TagRef{Name: "v0.3.0-beta", CommitSHA: "c2", ObjectSHA: "t2"},
				Writes: []string{createTag, createRef},
			},
		},
		{
			name: "ref raced into existence conflicts",
			spec: ports.TagSpec{Name: "v0.3.0-beta", Commit: "c2"},
			rt: routes{
				tagRefPath: missing, createTag: newObject,
				createRef: reply(http.StatusUnprocessableEntity, `{"message":"Reference already exists"}`),
			},
			want: tagOutcome{Writes: []string{createTag, createRef}, Kind: ports.ErrConflict},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, fake, _ := newClient(t, tc.rt)
			ref, err := c.UpsertTag(t.Context(), tc.spec)
			assert.Equal(t, tc.want, tagOutcome{Ref: ref, Writes: fake.writes(), Kind: kindOf(err)})
		})
	}
}

// TestUpsertTagBody pins an annotated tag on a commit: a lightweight tag has
// no message, and the tag message is what the release page shows.
func TestUpsertTagBody(t *testing.T) {
	c, fake, _ := newClient(t, routes{
		tagRefPath: reply(http.StatusNotFound, `{}`),
		createTag:  reply(http.StatusCreated, `{"sha":"t2"}`),
		createRef:  reply(http.StatusCreated, `{}`),
	})
	_, err := c.UpsertTag(t.Context(), ports.TagSpec{Name: "v0.3.0-beta", Commit: "c2", Message: "v0.3.0-beta"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		createTag: `{"tag":"v0.3.0-beta","message":"v0.3.0-beta","object":"c2","type":"commit"}` + "\n",
		createRef: `{"ref":"refs/tags/v0.3.0-beta","sha":"t2"}` + "\n",
	}, fake.bodies)
}

type commitOutcome struct {
	SHA    deploy.SHA
	Writes []string
	Kind   error
}

func TestCommitFiles(t *testing.T) {
	const (
		blobs   = "POST /repos/o/r/git/blobs"
		trees   = "POST /repos/o/r/git/trees"
		commits = "POST /repos/o/r/git/commits"
	)
	base := routes{
		"GET /repos/o/r/git/commits/base": reply(http.StatusOK, `{"sha":"base","tree":{"sha":"tree0"}}`),
		blobs:                             reply(http.StatusCreated, `{"sha":"blob1"}`),
		trees:                             reply(http.StatusCreated, `{"sha":"tree1"}`),
		commits:                           reply(http.StatusCreated, `{"sha":"new"}`),
	}
	cases := []struct {
		name string
		ref  http.HandlerFunc
		want commitOutcome
	}{
		{
			name: "new branch at the new commit",
			ref:  reply(http.StatusCreated, `{}`),
			want: commitOutcome{SHA: "new", Writes: []string{blobs, trees, commits, createRef}},
		},
		{
			name: "existing branch conflicts",
			ref:  reply(http.StatusUnprocessableEntity, `{"message":"Reference already exists"}`),
			want: commitOutcome{Writes: []string{blobs, trees, commits, createRef}, Kind: ports.ErrConflict},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := routes{createRef: tc.ref}
			for k, v := range base {
				rt[k] = v
			}
			c, fake, _ := newClient(t, rt)
			sha, err := c.CommitFiles(t.Context(), ports.CommitSpec{
				Branch: "chore/changelog-v0.3.0-beta", Base: "base", Message: "chore: changelog",
				Files: ports.Files{"web/marketing/src/content/changelog/v0.3.0-beta.json": []byte("{}\n")},
			})
			assert.Equal(t, tc.want, commitOutcome{SHA: sha, Writes: fake.writes(), Kind: kindOf(err)})
		})
	}
}

// TestCommitFilesBodies pins exact bytes (base64 blob) and a tree layered
// on the base commit's tree, so untouched files stay as they are.
func TestCommitFilesBodies(t *testing.T) {
	c, fake, _ := newClient(t, routes{
		"GET /repos/o/r/git/commits/base": reply(http.StatusOK, `{"sha":"base","tree":{"sha":"tree0"}}`),
		"POST /repos/o/r/git/blobs":       reply(http.StatusCreated, `{"sha":"blob1"}`),
		"POST /repos/o/r/git/trees":       reply(http.StatusCreated, `{"sha":"tree1"}`),
		"POST /repos/o/r/git/commits":     reply(http.StatusCreated, `{"sha":"new"}`),
		createRef:                         reply(http.StatusCreated, `{}`),
	})
	_, err := c.CommitFiles(t.Context(), ports.CommitSpec{
		Branch: "chore/deploy-v0.3.0-beta", Base: "base", Message: "Deploy",
		Files: ports.Files{"deploy/k8s/gossip.yaml": []byte("image: x\n")},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"POST /repos/o/r/git/blobs":   `{"content":"aW1hZ2U6IHgK","encoding":"base64"}` + "\n",
		"POST /repos/o/r/git/trees":   `{"base_tree":"tree0","tree":[{"sha":"blob1","path":"deploy/k8s/gossip.yaml","mode":"100644","type":"blob"}]}` + "\n",
		"POST /repos/o/r/git/commits": `{"message":"Deploy","tree":"tree1","parents":["base"]}` + "\n",
		createRef:                     `{"ref":"refs/heads/chore/deploy-v0.3.0-beta","sha":"new"}` + "\n",
	}, fake.bodies)
}

func TestCompare(t *testing.T) {
	c, _, _ := newClient(t, routes{
		"GET /repos/o/r/compare/live...pin": reply(http.StatusOK, `{"ahead_by":2,"behind_by":0,
			"commits":[
				{"sha":"s1","html_url":"u1","author":{"login":"adam"},"commit":{"message":"feat(time): lookup (#1006)\n\nbody"}},
				{"sha":"s2","html_url":"u2","commit":{"message":"Merge pull request #7 from a/b","author":{"name":"Adam"}}}],
			"files":[{"filename":"deploy/messaging/hub.yaml"},{"filename":"deploy/k8s/gossip.yaml"}]}`),
	})
	got, err := c.Compare(t.Context(), "live", "pin")
	require.NoError(t, err)
	assert.Equal(t, ports.Comparison{
		AheadBy: 2,
		Commits: []deploy.Commit{
			{SHA: "s1", Title: "feat(time): lookup (#1006)", Author: "adam", PR: 1006, URL: "u1"},
			{SHA: "s2", Title: "Merge pull request #7 from a/b", Author: "Adam", PR: 7, URL: "u2"},
		},
		Files: []ports.FilePath{"deploy/k8s/gossip.yaml", "deploy/messaging/hub.yaml"},
	}, got)
}

func TestToCommitPR(t *testing.T) {
	titles := map[string]int{
		"fix: y":                          0,
		"feat: x (#12)":                   12,
		"feat: x (#12) and more":          0,
		"Merge pull request #7 from a/b":  7,
		"Revert \"feat: x (#12)\" (#13)":  13,
		"chore(deploy): bump gossip (#9)": 9,
	}
	got := map[string]int{}
	for title := range titles {
		got[title] = toCommit(&github.RepositoryCommit{Commit: &github.Commit{Message: github.Ptr(title)}}).PR
	}
	assert.Equal(t, titles, got)
}

func TestDeleteBranch(t *testing.T) {
	cases := map[int]error{http.StatusNoContent: nil, http.StatusUnprocessableEntity: ports.ErrNotFound, http.StatusNotFound: ports.ErrNotFound}
	got := map[int]error{}
	for status := range cases {
		c, _, _ := newClient(t, routes{"DELETE /repos/o/r/git/refs/heads/chore/x": reply(status, `{}`)})
		got[status] = kindOf(c.DeleteBranch(t.Context(), "chore/x"))
	}
	assert.Equal(t, cases, got)
}
