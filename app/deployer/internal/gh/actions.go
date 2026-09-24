// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/google/go-github/v92/github"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/codec"
)

const activeWindow = 100

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

func (c *Client) RunJobs(ctx context.Context, runID int64) ([]ports.Job, error) {
	opts := &github.ListWorkflowJobsOptions{Filter: "latest", ListOptions: github.ListOptions{PerPage: 100}}
	jobs, err := collect(&opts.ListOptions, func() ([]*github.WorkflowJob, *github.Response, error) {
		res, resp, err := c.gh.Actions.ListWorkflowJobs(ctx, c.owner, c.repo, runID, opts)
		return res.GetJobs(), resp, err
	})
	return mapAll(jobs, toJob), err
}

// Use the bare client: the installation token must not reach the log storage host.
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

func (c *Client) AttestationExists(ctx context.Context, digest deploy.Digest) (bool, error) {
	u := fmt.Sprintf("repos/%s/%s/attestations/%s?predicate_type=provenance&per_page=1", c.owner, c.repo, digest)
	req, err := c.gh.NewRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	var res struct {
		Attestations []codec.RawMessage `json:"attestations"`
	}
	resp, err := c.gh.Do(req, &res)
	if ok, err := found(resp, err); !ok {
		return false, err
	}
	return len(res.Attestations) > 0, nil
}
