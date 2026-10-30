// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ports

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/rpc/deploy"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrNotFound       = errors.New("not found")
	ErrInvalid        = errors.New("invalid request")
	ErrConflict       = errors.New("conflict")
)

type Fail struct {
	deploy.Failure
}

func (f *Fail) Error() string { return string(f.Code) + ": " + f.Message }

func Failf(code deploy.FailureCode, format string, args ...any) *Fail {
	return &Fail{Failure: deploy.Failure{Code: code, Message: fmt.Sprintf(format, args...)}}
}

func AsFail(err error) (*Fail, bool) {
	var f *Fail
	ok := errors.As(err, &f)
	return f, ok
}

type (
	Branch   string
	Ref      string
	FilePath string
	Workflow string
	Event    string
)

type Files map[FilePath][]byte

type Config struct {
	Owner          string
	Repo           string
	MainBranch     Branch
	DeployBranch   Branch
	ImageRepo      string
	Workflow       Workflow
	CodeSceneCheck string

	ManifestDir  FilePath
	ChangelogDir FilePath

	UpdateBranchLimit int
	LogTailLines      int
	PollEvery         time.Duration
}

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type TagRef struct {
	Name      deploy.Version `json:"name"`
	CommitSHA deploy.SHA     `json:"commit_sha"`
	ObjectSHA deploy.SHA     `json:"object_sha"`
}

type TagSpec struct {
	Name    deploy.Version
	Commit  deploy.SHA
	Message string
	Force   bool
}

type Comparison struct {
	AheadBy  int
	BehindBy int
	Commits  []deploy.Commit
	Files    []FilePath
}

type Check struct {
	Name     string
	State    deploy.CheckState
	Required bool
	URL      string
	Summary  string
}

func (c Check) Evidence() []string {
	lines := []string{c.Name + ": " + c.URL}
	if c.Summary == "" {
		return lines
	}
	return append(lines, strings.Split(strings.TrimSpace(c.Summary), "\n")...)
}

type CheckSummary struct {
	State     deploy.CheckState
	CodeScene deploy.CheckState
	Checks    []Check
}

type PullRequest struct {
	Number     int
	Title      string
	Author     string
	URL        string
	HeadBranch Branch
	HeadSHA    deploy.SHA
	BaseBranch Branch
	Draft      bool
	Open       bool
	Merged     bool
	MergeSHA   deploy.SHA
	Mergeable  *bool
	Behind     bool
}

type PRSpec struct {
	Head  Branch
	Base  Branch
	Title string
	Body  string
}

type CommitSpec struct {
	Branch  Branch
	Base    deploy.SHA
	Message string
	Files   Files
}

type Release struct {
	ID              int64
	Tag             deploy.Version
	Title           string
	URL             string
	TargetCommitish string
	Latest          bool
	PublishedAt     time.Time
}

type ReleaseSpec struct {
	Tag    deploy.Version
	Title  string
	Notes  string
	Target deploy.SHA
}

type RunQuery struct {
	Workflow Workflow
	Event    Event
	Ref      Ref
	SHA      deploy.SHA
}

type WorkflowRun struct {
	ID         int64
	Number     int
	Status     string
	Conclusion string
	Attempt    int
	HeadSHA    deploy.SHA
	URL        string
	CreatedAt  time.Time
}

type Job struct {
	ID         int64
	Name       string
	Status     string
	Conclusion string
	URL        string
}

type GitHub interface {
	BranchHead(ctx context.Context, branch Branch) (deploy.SHA, error)
	MoveBranch(ctx context.Context, branch Branch, sha deploy.SHA) error
	LatestTag(ctx context.Context) (TagRef, error)
	Tag(ctx context.Context, name deploy.Version) (TagRef, bool, error)
	UpsertTag(ctx context.Context, spec TagSpec) (TagRef, error)
	Compare(ctx context.Context, base, head Ref) (Comparison, error)

	OpenPRs(ctx context.Context) ([]deploy.PRInfo, error)
	PR(ctx context.Context, number int) (PullRequest, error)
	FindPR(ctx context.Context, head Branch) (PullRequest, bool, error)
	CreatePR(ctx context.Context, spec PRSpec) (PullRequest, error)
	UpdateBranch(ctx context.Context, number int) error
	SquashMerge(ctx context.Context, number int, expectHead deploy.SHA) (deploy.SHA, error)
	DeleteBranch(ctx context.Context, branch Branch) error
	CommitFiles(ctx context.Context, spec CommitSpec) (deploy.SHA, error)
	Checks(ctx context.Context, sha deploy.SHA) (CheckSummary, error)

	File(ctx context.Context, path FilePath, ref Ref) ([]byte, error)
	Tree(ctx context.Context, dir FilePath, ref Ref) (Files, error)

	Release(ctx context.Context, tag deploy.Version) (Release, bool, error)
	UpsertRelease(ctx context.Context, spec ReleaseSpec) (Release, error)
	Releases(ctx context.Context, limit int) ([]Release, error)

	FindWorkflowRun(ctx context.Context, q RunQuery) (WorkflowRun, bool, error)
	WorkflowRun(ctx context.Context, id int64) (WorkflowRun, error)
	ActiveRuns(ctx context.Context, wf Workflow) ([]WorkflowRun, error)
	RunJobs(ctx context.Context, runID int64) ([]Job, error)
	JobLogTail(ctx context.Context, jobID int64, n int) ([]string, error)
	RerunFailedJobs(ctx context.Context, runID int64) error
	AttestationExists(ctx context.Context, digest deploy.Digest) (bool, error)
}

type ImageRef struct {
	Image deploy.ImageName
	Tag   deploy.Tag
}

type ImageInfo struct {
	Digest    deploy.Digest
	Platforms []string
	Revision  deploy.SHA
}

type Registry interface {
	Resolve(ctx context.Context, ref ImageRef) (ImageInfo, error)
	Tags(ctx context.Context, image deploy.ImageName) ([]deploy.Tag, error)
}
