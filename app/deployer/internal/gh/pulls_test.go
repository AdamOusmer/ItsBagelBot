// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const (
	pr5Path   = "GET /repos/o/r/pulls/5"
	mergePath = "PUT /repos/o/r/pulls/5/merge"
	openPR5   = `{"number":5,"title":"feat: x","state":"open","html_url":"u5","user":{"login":"adam"},
		"head":{"ref":"feat/x","sha":"h5"},"base":{"ref":"main"},"mergeable":true,"mergeable_state":"clean"}`
	mergedPR5 = `{"number":5,"title":"feat: x","state":"closed","merged_at":"2026-09-22T00:00:00Z",
		"merge_commit_sha":"m5","head":{"ref":"feat/x","sha":"h5"},"base":{"ref":"main"}}`
)

func TestOpenPRs(t *testing.T) {
	c, fake, _ := newClient(t, routes{
		"GET /repos/o/r/pulls": reply(http.StatusOK, `[{"number":5},{"number":6}]`),
		pr5Path:                reply(http.StatusOK, openPR5),
		"GET /repos/o/r/pulls/6": reply(http.StatusOK, `{"number":6,"title":"fix: y","state":"open","draft":true,
			"user":{"login":"bot"},"head":{"ref":"fix/y","sha":"h6"},"base":{"ref":"main"},"mergeable_state":"behind"}`),
		"GET /repos/o/r/rules/branches/main":   codesceneRule,
		"GET /repos/o/r/commits/h5/check-runs": reply(http.StatusOK, `{"check_runs":[{"name":"`+codescene+`","status":"completed","conclusion":"success"}]}`),
		"GET /repos/o/r/commits/h5/status":     reply(http.StatusOK, `{"statuses":[]}`),
		"GET /repos/o/r/commits/h6/check-runs": reply(http.StatusOK, `{"check_runs":[]}`),
		"GET /repos/o/r/commits/h6/status":     reply(http.StatusOK, `{"statuses":[]}`),
	})
	var calls []int
	var got []deploy.PRInfo
	for range 2 {
		prs, err := c.OpenPRs(t.Context())
		require.NoError(t, err)
		got = prs
		calls = append(calls, fake.count())
	}
	assert.Equal(t, []deploy.PRInfo{
		{Number: 5, Title: "feat: x", Author: "adam", URL: "u5", HeadSHA: "h5",
			Checks: deploy.ChecksSuccess, CodeScene: deploy.ChecksSuccess, Mergeable: true},
		{Number: 6, Title: "fix: y", Author: "bot", HeadSHA: "h6", Draft: true,
			Checks: deploy.ChecksPending, CodeScene: deploy.ChecksNone, Behind: true},
	}, got)
	assert.Equal(t, 0, calls[1]-calls[0])
}

type findOutcome struct {
	Number int
	Found  bool
	Kind   error
}

func TestFindPR(t *testing.T) {
	const (
		open   = `{"number":5,"state":"open"}`
		merged = `{"number":4,"state":"closed","merged_at":"2026-09-20T00:00:00Z"}`
		closed = `{"number":3,"state":"closed"}`
	)
	cases := []struct {
		name string
		list string
		want findOutcome
	}{
		{name: "open wins over merged", list: `[` + merged + `,` + open + `]`, want: findOutcome{Number: 5, Found: true}},
		{name: "merged counts", list: `[` + closed + `,` + merged + `]`, want: findOutcome{Number: 4, Found: true}},
		{name: "closed unmerged is abandoned", list: `[` + closed + `]`, want: findOutcome{}},
		{name: "no PR", list: `[]`, want: findOutcome{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var query string
			c, _, _ := newClient(t, routes{
				"GET /repos/o/r/pulls": func(w http.ResponseWriter, r *http.Request) {
					query = r.URL.Query().Get("head") + " " + r.URL.Query().Get("state")
					reply(http.StatusOK, tc.list)(w, r)
				},
				"GET /repos/o/r/pulls/4": reply(http.StatusOK, merged),
				pr5Path:                  reply(http.StatusOK, openPR5),
			})
			pr, ok, err := c.FindPR(t.Context(), "chore/deploy-v0.3.0-beta")
			assert.Equal(t, tc.want, findOutcome{Number: pr.Number, Found: ok, Kind: kindOf(err)})
			assert.Equal(t, "o:chore/deploy-v0.3.0-beta all", query)
		})
	}
}

func TestPR(t *testing.T) {
	c, _, _ := newClient(t, routes{pr5Path: reply(http.StatusOK, mergedPR5)})
	pr, err := c.PR(t.Context(), 5)
	require.NoError(t, err)
	assert.Equal(t, ports.PullRequest{
		Number: 5, Title: "feat: x", HeadBranch: "feat/x", HeadSHA: "h5", BaseBranch: "main", Merged: true, MergeSHA: "m5",
	}, pr)
}

type mergeOutcome struct {
	SHA    deploy.SHA
	Writes []string
	Kind   error
}

func TestSquashMerge(t *testing.T) {
	cases := []struct {
		name  string
		pr    string
		merge http.HandlerFunc
		want  mergeOutcome
	}{
		{
			name: "already merged returns its merge commit",
			pr:   mergedPR5,
			want: mergeOutcome{SHA: "m5"},
		},
		{
			name:  "merges",
			pr:    openPR5,
			merge: reply(http.StatusOK, `{"sha":"s5","merged":true}`),
			want:  mergeOutcome{SHA: "s5", Writes: []string{mergePath}},
		},
		{
			name:  "head moved",
			pr:    openPR5,
			merge: reply(http.StatusConflict, `{"message":"Head branch was modified"}`),
			want:  mergeOutcome{Writes: []string{mergePath}, Kind: ports.ErrConflict},
		},
		{
			name:  "not mergeable",
			pr:    openPR5,
			merge: reply(http.StatusMethodNotAllowed, `{"message":"Required status check is expected"}`),
			want:  mergeOutcome{Writes: []string{mergePath}, Kind: ports.ErrConflict},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := routes{pr5Path: reply(http.StatusOK, tc.pr)}
			if tc.merge != nil {
				rt[mergePath] = tc.merge
			}
			c, fake, _ := newClient(t, rt)
			sha, err := c.SquashMerge(t.Context(), 5, "h5")
			assert.Equal(t, tc.want, mergeOutcome{SHA: sha, Writes: fake.writes(), Kind: kindOf(err)})
		})
	}
}

func TestSquashMergeBody(t *testing.T) {
	c, fake, _ := newClient(t, routes{
		pr5Path:   reply(http.StatusOK, openPR5),
		mergePath: reply(http.StatusOK, `{"sha":"s5","merged":true}`),
	})
	_, err := c.SquashMerge(t.Context(), 5, "h5")
	require.NoError(t, err)
	assert.JSONEq(t, `{"commit_title":"feat: x (#5)","sha":"h5","merge_method":"squash"}`, fake.bodies[mergePath])
}

func TestPRWrites(t *testing.T) {
	cases := []struct {
		name string
		rt   routes
		call func(*Client, *testing.T) error
		want error
	}{
		{
			name: "update branch accepted in the background",
			rt:   routes{"PUT /repos/o/r/pulls/5/update-branch": reply(http.StatusAccepted, `{"message":"Updating pull request branch."}`)},
			call: func(c *Client, t *testing.T) error { return c.UpdateBranch(t.Context(), 5) },
		},
		{
			name: "update branch refused",
			rt:   routes{"PUT /repos/o/r/pulls/5/update-branch": reply(http.StatusUnprocessableEntity, `{"message":"merge conflict"}`)},
			call: func(c *Client, t *testing.T) error { return c.UpdateBranch(t.Context(), 5) },
			want: errOther,
		},
		{
			name: "create PR",
			rt:   routes{"POST /repos/o/r/pulls": reply(http.StatusCreated, openPR5)},
			call: createPR,
		},
		{
			name: "create PR that already exists",
			rt:   routes{"POST /repos/o/r/pulls": reply(http.StatusUnprocessableEntity, `{"message":"A pull request already exists"}`)},
			call: createPR,
			want: ports.ErrConflict,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := newClient(t, tc.rt)
			assert.Equal(t, tc.want, kindOf(tc.call(c, t)))
		})
	}
}

func createPR(c *Client, t *testing.T) error {
	_, err := c.CreatePR(t.Context(), ports.PRSpec{Head: "chore/x", Base: "main", Title: "t", Body: "b"})
	return err
}
