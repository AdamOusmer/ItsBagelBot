// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ports

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"ItsBagelBot/internal/domain/rpc/deploy"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrNotFound       = errors.New("not found")
	ErrInvalid        = errors.New("invalid request")
	ErrForbidden      = errors.New("forbidden: owner role required")
	ErrConflict       = errors.New("conflict")
	ErrLockHeld       = errors.New("deploy lock held by another run")
	ErrNotResumable   = errors.New("run is not in a resumable state")
	ErrRefused        = errors.New("refused by allowlist")
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
	Branch    string
	Ref       string
	FilePath  string
	Namespace string
	Workflow  string
	Event     string
	URL       string
)

type Files map[FilePath][]byte

type Config struct {
	Owner          string
	Repo           string
	MainBranch     Branch
	ImageRepo      string
	Workflow       Workflow
	CodeSceneCheck string

	ManifestDir     FilePath
	MessagingDir    FilePath
	PriorityClasses FilePath
	StatusRoutes    FilePath
	ChangelogDir    FilePath

	RolloutTimeout        time.Duration
	ACLTimeout            time.Duration
	ACLSettle             time.Duration
	FailedSchedulingAfter time.Duration
	RestartLimit          int32
	UpdateBranchLimit     int
	LogTailLines          int
	PollEvery             time.Duration
	LockTTL               time.Duration
	HeartbeatEvery        time.Duration
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

type ObjectRef struct {
	Kind      string
	Namespace Namespace
	Name      string
}

type Objects []*unstructured.Unstructured

type BuildSpec struct {
	Files Files
	Root  FilePath
}

type LintFinding struct {
	Object ObjectRef
	Reason string
}

type ApplyResult struct {
	Applied []ObjectRef
	Changed []ObjectRef
}

type Applier interface {
	Build(ctx context.Context, spec BuildSpec) (Objects, error)
	Lint(objs Objects) []LintFinding
	Apply(ctx context.Context, objs Objects) (ApplyResult, error)
}

type WorkloadRef struct {
	Kind      string
	Namespace Namespace
	Name      string
}

type Unsettled struct {
	Workload WorkloadRef
	Reason   string
}

type LiveImage struct {
	Workload  WorkloadRef
	Container string
	Image     string
}

type Pins map[deploy.ImageName]deploy.Digest

type RolloutSpec struct {
	Workload WorkloadRef
	Pins     Pins
}

type ProgressFunc func(deploy.Item)

type Mismatch struct {
	Workload  WorkloadRef
	Pod       string
	Container string
	Want      deploy.Digest
	Got       deploy.Digest
}

type NATSServer struct {
	Pod          string
	Node         string
	ConfigLoaded time.Time
}

type Watcher interface {
	Reachable(ctx context.Context) error
	Settled(ctx context.Context, refs []WorkloadRef) ([]Unsettled, error)
	LiveImages(ctx context.Context, refs []WorkloadRef) ([]LiveImage, error)
	WaitRollout(ctx context.Context, spec RolloutSpec, progress ProgressFunc) error
	VerifyImageIDs(ctx context.Context, refs []WorkloadRef, pins Pins) ([]Mismatch, error)
	NATSServers(ctx context.Context) ([]NATSServer, error)
	Probe(ctx context.Context, url URL) (int, error)
}

type Revision uint64

type Lock struct {
	Owner    deploy.RunID
	Revision Revision
}

type Store interface {
	Get(ctx context.Context, id deploy.RunID) (deploy.Run, Revision, error)
	Put(ctx context.Context, run *deploy.Run, rev Revision) (Revision, error)
	Active(ctx context.Context) (deploy.Run, Revision, bool, error)
	List(ctx context.Context, limit int) ([]deploy.RunSummary, error)

	AcquireLock(ctx context.Context, owner deploy.RunID) (Lock, error)
	Heartbeat(ctx context.Context, lock Lock) (Lock, error)
	ReleaseLock(ctx context.Context, lock Lock) error
	LockOwner(ctx context.Context) (deploy.RunID, bool, error)
}

type Events interface {
	Publish(ctx context.Context, run *deploy.Run) error
}

type Authorizer interface {
	RequireOwner(ctx context.Context, actorID string) (deploy.Actor, error)
}
