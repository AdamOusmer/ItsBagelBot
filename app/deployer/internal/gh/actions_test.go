// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
)

const runsPath = "GET /repos/o/r/actions/workflows/publish-images.yml/runs"

type runOutcome struct {
	Run   ports.WorkflowRun
	Found bool
	Calls []string
}

func TestFindWorkflowRun(t *testing.T) {
	created := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		q    ports.RunQuery
		body string
		want runOutcome
	}{
		{
			name: "tag push filters by tag name as the branch",
			q:    ports.RunQuery{Workflow: "publish-images.yml", Event: "push", Ref: "v0.3.0-beta", SHA: "c1"},
			body: `{"total_count":1,"workflow_runs":[{"id":9,"run_number":41,"status":"in_progress","head_sha":"c1",
				"html_url":"u9","created_at":"2026-09-23T10:00:00Z"}]}`,
			want: runOutcome{
				Run:   ports.WorkflowRun{ID: 9, Number: 41, Status: "in_progress", HeadSHA: "c1", URL: "u9", CreatedAt: created},
				Found: true,
				Calls: []string{runsPath + "?branch=v0.3.0-beta&event=push&head_sha=c1&per_page=1"},
			},
		},
		{
			name: "not started yet",
			q:    ports.RunQuery{Workflow: "publish-images.yml", Event: "push", Ref: "main", SHA: "c2"},
			body: `{"total_count":0,"workflow_runs":[]}`,
			want: runOutcome{Calls: []string{runsPath + "?branch=main&event=push&head_sha=c2&per_page=1"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, fake, _ := newClient(t, routes{runsPath: reply(http.StatusOK, tc.body)})
			run, ok, err := c.FindWorkflowRun(t.Context(), tc.q)
			require.NoError(t, err)
			assert.Equal(t, tc.want, runOutcome{Run: run, Found: ok, Calls: fake.calls})
		})
	}
}

func TestActiveRuns(t *testing.T) {
	c, _, _ := newClient(t, routes{runsPath: reply(http.StatusOK, `{"workflow_runs":[
		{"id":3,"status":"queued","created_at":"2026-09-23T10:03:00Z"},
		{"id":2,"status":"completed","conclusion":"success","created_at":"2026-09-23T10:02:00Z"},
		{"id":1,"status":"in_progress","created_at":"2026-09-23T10:01:00Z"}]}`)})
	runs, err := c.ActiveRuns(t.Context(), "publish-images.yml")
	require.NoError(t, err)
	ids := mapAll(runs, func(r ports.WorkflowRun) int64 { return r.ID })
	assert.Equal(t, []int64{1, 3}, ids)
}

func TestRunJobs(t *testing.T) {
	c, fake, _ := newClient(t, routes{"GET /repos/o/r/actions/runs/9/jobs": reply(http.StatusOK, `{"total_count":2,"jobs":[
		{"id":1,"name":"Build gossip arm64","status":"completed","conclusion":"success","html_url":"j1"},
		{"id":2,"name":"Build gossip amd64","status":"in_progress","html_url":"j2"}]}`)})
	jobs, err := c.RunJobs(t.Context(), 9)
	require.NoError(t, err)
	assert.Equal(t, []any{
		[]ports.Job{
			{ID: 1, Name: "Build gossip arm64", Status: "completed", Conclusion: "success", URL: "j1"},
			{ID: 2, Name: "Build gossip amd64", Status: "in_progress", URL: "j2"},
		},
		[]string{"GET /repos/o/r/actions/runs/9/jobs?filter=latest&page=1&per_page=100"},
	}, []any{jobs, fake.calls})
}

func TestJobLogTail(t *testing.T) {
	var storageAuth string
	var base string
	c, _, _ := newClient(t, routes{
		"GET /repos/o/r/actions/jobs/7/logs": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, base+"/storage/job-7.txt?sig=x", http.StatusFound)
		},
		"GET /storage/job-7.txt": func(w http.ResponseWriter, r *http.Request) {
			storageAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte("step 1\r\nstep 2\nerror: build failed\n"))
		},
	})
	base = strings.TrimSuffix(c.gh.BaseURL(), "/")
	lines, err := c.JobLogTail(t.Context(), 7, 2)
	require.NoError(t, err)
	assert.Equal(t, []any{[]string{"step 2", "error: build failed"}, ""}, []any{lines, storageAuth})
}

func TestJobLogTailStorageError(t *testing.T) {
	var base string
	c, _, _ := newClient(t, routes{
		"GET /repos/o/r/actions/jobs/7/logs": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, base+"/storage/gone", http.StatusFound)
		},
		"GET /storage/gone": reply(http.StatusForbidden, `expired`),
	})
	base = strings.TrimSuffix(c.gh.BaseURL(), "/")
	_, err := c.JobLogTail(t.Context(), 7, 50)
	assert.ErrorContains(t, err, "403")
}

func TestTailLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want []string
	}{
		{name: "none asked", in: "a\nb\n", n: 0, want: nil},
		{name: "fewer lines than asked", in: "a\nb\n", n: 5, want: []string{"a", "b"}},
		{name: "ring wraps", in: "a\nb\nc\nd\ne\n", n: 2, want: []string{"d", "e"}},
		{name: "ring wraps unevenly", in: "a\nb\nc\nd\ne", n: 3, want: []string{"c", "d", "e"}},
		{name: "crlf trimmed", in: "a\r\nb\r\n", n: 2, want: []string{"a", "b"}},
		{name: "empty log", in: "", n: 3, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tailLines(strings.NewReader(tc.in), tc.n)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTailLinesLongLine(t *testing.T) {
	long := strings.Repeat("x", 200<<10)
	got, err := tailLines(strings.NewReader(long+"\nlast\n"), 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"last"}, got)
}

func TestRerunFailedJobs(t *testing.T) {
	c, fake, _ := newClient(t, routes{"POST /repos/o/r/actions/runs/9/rerun-failed-jobs": reply(http.StatusCreated, `{}`)})
	require.NoError(t, c.RerunFailedJobs(t.Context(), 9))
	assert.Equal(t, []string{"POST /repos/o/r/actions/runs/9/rerun-failed-jobs"}, fake.writes())
}

type attestOutcome struct {
	Exists bool
	Query  string
	Kind   error
}

func TestAttestationExists(t *testing.T) {
	const query = "predicate_type=provenance&per_page=1"
	cases := []struct {
		name string
		h    http.HandlerFunc
		want attestOutcome
	}{
		{
			name: "provenance attested",
			h:    reply(http.StatusOK, `{"attestations":[{"repository_id":1,"bundle_url":"https://example.invalid/b"}]}`),
			want: attestOutcome{Exists: true, Query: query},
		},
		{
			name: "none",
			h:    reply(http.StatusOK, `{"attestations":[]}`),
			want: attestOutcome{Query: query},
		},
		{
			name: "digest unknown",
			h:    reply(http.StatusNotFound, `{"message":"Not Found"}`),
			want: attestOutcome{Query: query},
		},
		{
			name: "api failure",
			h:    reply(http.StatusInternalServerError, `{"message":"boom"}`),
			want: attestOutcome{Query: query, Kind: errOther},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var q string
			c, _, _ := newClient(t, routes{"GET /repos/o/r/attestations/sha256:abc": func(w http.ResponseWriter, r *http.Request) {
				q = r.URL.RawQuery
				tc.h(w, r)
			}})
			ok, err := c.AttestationExists(t.Context(), "sha256:abc")
			assert.Equal(t, tc.want, attestOutcome{Exists: ok, Query: q, Kind: kindOf(err)})
		})
	}
}
