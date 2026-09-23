// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/google/go-github/v92/github"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// activeWindow is how many of a workflow's newest runs ActiveRuns scans.
// The API filters by one status per call, and a run held by a concurrency
// group reports pending or waiting rather than queued, so exact filtering
// is five calls a poll: 3600 an hour at the 5 s cadence, most of the
// installation's 5000. Active runs are always among the newest, and a
// release starts one publish-images run, so one call over 100 finds them.
const activeWindow = 100

// maxLogLine is the longest log line tailLines accepts. bufio.Scanner's
// 64 KB default fails the whole read on one longer line, which would turn a
// build failure into a missing log tail; 1 MB caps what one line may cost.
const maxLogLine = 1 << 20

func toRun(r *github.WorkflowRun) ports.WorkflowRun {
	return ports.WorkflowRun{
		ID:         r.GetID(),
		Number:     r.GetRunNumber(),
		Status:     r.GetStatus(),
		Conclusion: r.GetConclusion(),
		Attempt:    r.GetRunAttempt(),
		HeadSHA:    deploy.SHA(r.GetHeadSHA()),
		URL:        r.GetHTMLURL(),
		CreatedAt:  r.GetCreatedAt().Time,
	}
}

func toJob(j *github.WorkflowJob) ports.Job {
	return ports.Job{
		ID: j.GetID(), Name: j.GetName(), Status: j.GetStatus(), Conclusion: j.GetConclusion(), URL: j.GetHTMLURL(),
	}
}

// FindWorkflowRun returns the newest matching run. head_branch on a tag
// push is the tag name, so Ref filters tag and branch pushes alike, and a
// rerun of failed jobs keeps its run id, so the newest is the one to watch.
func (c *Client) FindWorkflowRun(ctx context.Context, q ports.RunQuery) (ports.WorkflowRun, bool, error) {
	runs, resp, err := c.gh.Actions.ListWorkflowRunsByFileName(ctx, c.owner, c.repo, string(q.Workflow),
		&github.ListWorkflowRunsOptions{
			Event: string(q.Event), Branch: string(q.Ref), HeadSHA: string(q.SHA),
			ListOptions: github.ListOptions{PerPage: 1},
		})
	if err != nil {
		return ports.WorkflowRun{}, false, apiErr(resp, err)
	}
	if len(runs.GetWorkflowRuns()) == 0 {
		return ports.WorkflowRun{}, false, nil
	}
	return toRun(runs.GetWorkflowRuns()[0]), true, nil
}

func (c *Client) WorkflowRun(ctx context.Context, id int64) (ports.WorkflowRun, error) {
	run, resp, err := c.gh.Actions.GetWorkflowRunByID(ctx, c.owner, c.repo, id)
	if err != nil {
		return ports.WorkflowRun{}, apiErr(resp, err)
	}
	return toRun(run), nil
}

func (c *Client) ActiveRuns(ctx context.Context, wf ports.Workflow) ([]ports.WorkflowRun, error) {
	runs, resp, err := c.gh.Actions.ListWorkflowRunsByFileName(ctx, c.owner, c.repo, string(wf),
		&github.ListWorkflowRunsOptions{ListOptions: github.ListOptions{PerPage: activeWindow}})
	if err != nil {
		return nil, apiErr(resp, err)
	}
	var out []ports.WorkflowRun
	for _, r := range runs.GetWorkflowRuns() {
		if r.GetStatus() != "completed" {
			out = append(out, toRun(r))
		}
	}
	slices.SortFunc(out, func(a, b ports.WorkflowRun) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}

// RunJobs lists the latest attempt's jobs, so a rerun of failed jobs shows
// its fresh attempts rather than the failures it replaced.
func (c *Client) RunJobs(ctx context.Context, runID int64) ([]ports.Job, error) {
	opts := &github.ListWorkflowJobsOptions{Filter: "latest", ListOptions: github.ListOptions{PerPage: 100}}
	jobs, err := collect(&opts.ListOptions, func() ([]*github.WorkflowJob, *github.Response, error) {
		res, resp, err := c.gh.Actions.ListWorkflowJobs(ctx, c.owner, c.repo, runID, opts)
		return res.GetJobs(), resp, err
	})
	return mapAll(jobs, toJob), err
}

// JobLogTail follows GitHub's 302 to a short-lived pre-signed storage URL.
// That URL carries its own credential, so it is fetched with the bare
// client: sending the installation token to a storage host would leak it.
func (c *Client) JobLogTail(ctx context.Context, jobID int64, n int) ([]string, error) {
	u, resp, err := c.gh.Actions.GetWorkflowJobLogs(ctx, c.owner, c.repo, jobID, 1)
	if err != nil {
		return nil, apiErr(resp, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	res, err := c.raw.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("job %d log download: %s", jobID, res.Status)
	}
	return tailLines(res.Body, n)
}

// tailLines keeps the last n lines in a ring, so a multi-MB build log never
// sits in memory whole.
func tailLines(r io.Reader, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	ring := make([]string, 0, n)
	next := 0
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLogLine)
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if len(ring) < n {
			ring = append(ring, line)
			continue
		}
		ring[next] = line
		next = (next + 1) % n
	}
	return slices.Concat(ring[next:], ring[:next]), sc.Err()
}

func (c *Client) RerunFailedJobs(ctx context.Context, runID int64) error {
	resp, err := c.gh.Actions.RerunFailedJobsByID(ctx, c.owner, c.repo, runID)
	return apiErr(resp, err)
}

// AttestationExists filters by predicate server side. The list response
// carries bundle_url and no inline bundle (REST docs schema read 2026-09-23:
// repository_id, bundle_url, initiator), so the predicate cannot be read off
// the listing without a second fetch per attestation; predicate_type=provenance
// is GitHub's alias for the SLSA provenance that actions/attest-build-provenance
// writes. go-github's ListAttestations has no predicate_type option, hence the
// raw request. The Sigstore signature is verified by GitHub when the
// attestation is written (it is bound to the workflow's OIDC identity);
// verifying it again here would pull sigstore-go into the deployer for no
// stronger claim than "GitHub's own API says this repo attested this digest".
func (c *Client) AttestationExists(ctx context.Context, digest deploy.Digest) (bool, error) {
	u := fmt.Sprintf("repos/%s/%s/attestations/%s?predicate_type=provenance&per_page=1", c.owner, c.repo, digest)
	req, err := c.gh.NewRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	var res struct {
		Attestations []json.RawMessage `json:"attestations"`
	}
	resp, err := c.gh.Do(req, &res)
	if ok, err := found(resp, err); !ok {
		return false, err
	}
	return len(res.Attestations) > 0, nil
}
