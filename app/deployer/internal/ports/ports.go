// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package ports is the seam between the deployer's run engine and the
// outside world: GitHub, the image registry, the cluster and NATS. The
// engine and the stages depend only on these interfaces, so every stage is
// testable against fakes and every adapter is replaceable without touching
// train logic.
//
// Parameters are named types wherever two strings could trade places (a
// branch and a ref, a namespace and a name): the adapters shuttle all of them
// between three APIs and a transposition compiles fine as bare strings.
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

// Sentinel errors shared by the engine, the rpc layer and the adapters. The
// rpc layer maps them onto the rpc Code vocabulary.
var (
	// ErrNotImplemented is what a test fake returns for a call its case never
	// expects.
	ErrNotImplemented = errors.New("not implemented")
	// ErrNotFound: the addressed run, PR, file, tag, release or workflow run does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalid: the request is malformed (unknown kind, bad version, missing field).
	ErrInvalid = errors.New("invalid request")
	// ErrForbidden: the actor is not active staff with the owner role.
	ErrForbidden = errors.New("forbidden: owner role required")
	// ErrConflict: a KV revision CAS lost, or a run is already active.
	ErrConflict = errors.New("conflict")
	// ErrLockHeld: another run holds the cluster lock.
	ErrLockHeld = errors.New("deploy lock held by another run")
	// ErrNotResumable: resume/approve/cancel on a run in a state that does not allow it.
	ErrNotResumable = errors.New("run is not in a resumable state")
	// ErrRefused: the applier's allowlist rejected an object (kind or namespace).
	ErrRefused = errors.New("refused by allowlist")
)

// Fail is the error a stage (or an adapter on a stage's behalf) returns when
// the failure is meaningful to the operator: it carries the code, the
// message, a log tail and the actions to offer. Any other error is recorded
// as deploy.FailInternal with err.Error() as the message.
type Fail struct {
	deploy.Failure
}

func (f *Fail) Error() string { return string(f.Code) + ": " + f.Message }

// Failf builds a Fail with a formatted message and no log tail.
func Failf(code deploy.FailureCode, format string, args ...any) *Fail {
	return &Fail{Failure: deploy.Failure{Code: code, Message: fmt.Sprintf(format, args...)}}
}

// AsFail extracts a *Fail from an error chain.
func AsFail(err error) (*Fail, bool) {
	var f *Fail
	ok := errors.As(err, &f)
	return f, ok
}

// Named string types for adapter parameters.
type (
	Branch    string // git branch name, e.g. "main", "chore/deploy-v0.2.3-beta"
	Ref       string // any git ref accepted by the GitHub contents API: branch, tag or sha
	FilePath  string // repo-relative path with forward slashes
	Namespace string
	Workflow  string // workflow file name, e.g. "publish-images.yml"
	Event     string // workflow trigger event, e.g. "push"
	URL       string
)

// Files is a set of repo-relative files and their bytes.
type Files map[FilePath][]byte

// Config is the deployer's tuning and repository identity. Built by
// internal/config from env; defaults are in the field comments.
type Config struct {
	Owner      string   // GitHub owner, "AdamOusmer"
	Repo       string   // GitHub repo, "ItsBagelBot"
	MainBranch Branch   // "main"
	ImageRepo  string   // "ghcr.io/adamousmer/itsbagelbot"
	Workflow   Workflow // "publish-images.yml"
	// CodeSceneCheck is the check run name merge_prs requires green on
	// top of the ruleset's required checks.
	CodeSceneCheck string // "CodeScene Code Health Review (main)"

	ManifestDir     FilePath // "deploy/k8s"
	MessagingDir    FilePath // "deploy/messaging"
	PriorityClasses FilePath // "deploy/k8s/priorityclasses.yaml"
	StatusRoutes    FilePath // "deploy/db/status-routes.yaml"
	ChangelogDir    FilePath // "web/marketing/src/content/changelog"

	RolloutTimeout time.Duration // 8m per service
	ACLTimeout     time.Duration // 3m
	// FailedSchedulingAfter is how old a FailedScheduling event must be
	// before rollout fast-fails on it (the scheduler retries briefly).
	FailedSchedulingAfter time.Duration // 60s
	RestartLimit          int32         // 3: a new-ReplicaSet pod at this restartCount fast-fails
	UpdateBranchLimit     int           // 3: merge_prs update-branch attempts per PR
	LogTailLines          int           // 50
	PollEvery             time.Duration // 5s: GitHub and cluster poll cadence
	LockTTL               time.Duration // 2m after the last heartbeat
	HeartbeatEvery        time.Duration // 20s
}

// Clock is time, injectable for tests.
type Clock interface {
	Now() time.Time
}

// SystemClock is the real clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

// ---- GitHub ----

// TagRef is an annotated (or lightweight) tag.
type TagRef struct {
	Name      deploy.Version `json:"name"`
	CommitSHA deploy.SHA     `json:"commit_sha"` // peeled target commit
	ObjectSHA deploy.SHA     `json:"object_sha"` // tag object sha (== CommitSHA for lightweight)
}

// TagSpec creates or force-moves an annotated tag.
type TagSpec struct {
	Name    deploy.Version
	Commit  deploy.SHA
	Message string
	// Force moves an existing tag; without it an existing tag at a different
	// commit is ErrConflict.
	Force bool
}

// Comparison is GitHub's compare base...head.
type Comparison struct {
	AheadBy  int
	BehindBy int
	Commits  []deploy.Commit
	Files    []FilePath // changed paths
}

// Check is one check run or commit status on a sha.
type Check struct {
	Name     string
	State    deploy.CheckState
	Required bool
	URL      string
	// Summary is the check run's output.summary when the run failed, empty
	// otherwise. CodeScene lists the flagged functions there, which is the
	// part of a red check a reader acts on; the link alone costs a click into
	// the dashboard for every failure.
	Summary string
}

// Evidence is one check's lines in a failure's log tail: name and link, then
// its summary line by line.
func (c Check) Evidence() []string {
	lines := []string{c.Name + ": " + c.URL}
	if c.Summary == "" {
		return lines
	}
	return append(lines, strings.Split(strings.TrimSpace(c.Summary), "\n")...)
}

// CheckSummary aggregates the checks on one sha. State considers only
// required checks plus Config.CodeSceneCheck; a required check that has not
// reported yet counts as pending.
type CheckSummary struct {
	State     deploy.CheckState
	CodeScene deploy.CheckState
	Checks    []Check
}

// PullRequest is the PR fields the train needs.
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
	MergeSHA   deploy.SHA // set when Merged
	// Mergeable is GitHub's computed mergeability; nil while GitHub is still
	// computing it (retry after a poll).
	Mergeable *bool
	Behind    bool // mergeable_state == "behind"
}

// PRSpec opens a PR.
type PRSpec struct {
	Head  Branch
	Base  Branch
	Title string
	Body  string
}

// CommitSpec creates Branch at Base plus one commit writing Files (git data
// API: blobs, tree, commit, ref). An existing Branch is ErrConflict.
type CommitSpec struct {
	Branch  Branch
	Base    deploy.SHA
	Message string
	Files   Files
}

// Release is a GitHub release.
type Release struct {
	ID              int64
	Tag             deploy.Version
	Title           string
	URL             string
	TargetCommitish string
	Latest          bool
	PublishedAt     time.Time
}

// ReleaseSpec creates or updates the release for Tag, marked Latest.
type ReleaseSpec struct {
	Tag    deploy.Version
	Title  string
	Notes  string
	Target deploy.SHA
}

// RunQuery finds a workflow run.
type RunQuery struct {
	Workflow Workflow
	Event    Event
	Ref      Ref // branch name for a branch push, tag name for a tag push
	SHA      deploy.SHA
}

// WorkflowRun is a GitHub Actions run.
type WorkflowRun struct {
	ID         int64
	Number     int
	Status     string // queued, in_progress, completed, waiting, pending, requested
	Conclusion string // success, failure, cancelled, ... when completed
	Attempt    int    // run_attempt: 1 on the first run, +1 per rerun
	HeadSHA    deploy.SHA
	URL        string
	CreatedAt  time.Time
}

// Job is one job of a workflow run.
type Job struct {
	ID         int64
	Name       string
	Status     string
	Conclusion string
	URL        string
}

// GitHub is the repository surface, authenticated as the deployer's GitHub
// App installation (not a ruleset bypass actor: merges pass the same gates a
// human's do). Missing things are ErrNotFound unless a bool says otherwise.
type GitHub interface {
	// BranchHead returns the commit a branch points at.
	BranchHead(ctx context.Context, branch Branch) (deploy.SHA, error)
	// LatestTag returns the highest vX.Y.Z-beta tag by semver.
	LatestTag(ctx context.Context) (TagRef, error)
	// Tag looks one tag up.
	Tag(ctx context.Context, name deploy.Version) (TagRef, bool, error)
	// UpsertTag creates an annotated tag, or force-moves it with spec.Force.
	UpsertTag(ctx context.Context, spec TagSpec) (TagRef, error)
	// Compare is base...head.
	Compare(ctx context.Context, base, head Ref) (Comparison, error)

	// OpenPRs lists open PRs against main with their check summary.
	OpenPRs(ctx context.Context) ([]deploy.PRInfo, error)
	PR(ctx context.Context, number int) (PullRequest, error)
	// FindPR finds the open or merged PR whose head is branch.
	FindPR(ctx context.Context, head Branch) (PullRequest, bool, error)
	CreatePR(ctx context.Context, spec PRSpec) (PullRequest, error)
	UpdateBranch(ctx context.Context, number int) error
	// SquashMerge merges with the repo convention: subject "<title> (#N)".
	// expectHead guards against a push landing between the checks and the merge.
	SquashMerge(ctx context.Context, number int, expectHead deploy.SHA) (deploy.SHA, error)
	DeleteBranch(ctx context.Context, branch Branch) error
	CommitFiles(ctx context.Context, spec CommitSpec) (deploy.SHA, error)
	Checks(ctx context.Context, sha deploy.SHA) (CheckSummary, error)

	// File reads one file at ref.
	File(ctx context.Context, path FilePath, ref Ref) ([]byte, error)
	// Tree reads every file under dir (recursive) at ref.
	Tree(ctx context.Context, dir FilePath, ref Ref) (Files, error)

	Release(ctx context.Context, tag deploy.Version) (Release, bool, error)
	UpsertRelease(ctx context.Context, spec ReleaseSpec) (Release, error)
	// Releases lists published releases, newest first.
	Releases(ctx context.Context, limit int) ([]Release, error)

	FindWorkflowRun(ctx context.Context, q RunQuery) (WorkflowRun, bool, error)
	WorkflowRun(ctx context.Context, id int64) (WorkflowRun, error)
	// ActiveRuns lists queued and in-progress runs of a workflow, oldest first.
	ActiveRuns(ctx context.Context, wf Workflow) ([]WorkflowRun, error)
	RunJobs(ctx context.Context, runID int64) ([]Job, error)
	// JobLogTail returns the last n lines of a job's log.
	JobLogTail(ctx context.Context, jobID int64, n int) ([]string, error)
	RerunFailedJobs(ctx context.Context, runID int64) error
	// AttestationExists reports whether a build provenance attestation
	// exists in the repo for the digest.
	AttestationExists(ctx context.Context, digest deploy.Digest) (bool, error)
}

// ---- Registry ----

// ImageRef names one tag of one first-party image.
type ImageRef struct {
	Image deploy.ImageName
	Tag   deploy.Tag
}

// ImageInfo is what the registry says about a tag.
type ImageInfo struct {
	// Digest is the manifest list (image index) digest.
	Digest deploy.Digest
	// Platforms are "os/arch" entries of the index, e.g. "linux/amd64".
	Platforms []string
	// Revision is the org.opencontainers.image.revision config label, read
	// from every platform image; empty when they disagree.
	Revision deploy.SHA
}

// Registry reads ghcr.io with the ghcr-pull read credential.
type Registry interface {
	Resolve(ctx context.Context, ref ImageRef) (ImageInfo, error)
	// Tags lists every tag of an image.
	Tags(ctx context.Context, image deploy.ImageName) ([]deploy.Tag, error)
}

// ---- Cluster: apply ----

// ObjectRef names one Kubernetes object.
type ObjectRef struct {
	Kind      string
	Namespace Namespace
	Name      string
}

// Objects are built manifests.
type Objects []*unstructured.Unstructured

// BuildSpec is an in-memory kustomization: Files keyed by repo-relative path
// and Root, the directory holding kustomization.yaml. A Root without a
// kustomization.yaml builds every *.yaml under it as plain manifests.
type BuildSpec struct {
	Files Files
	Root  FilePath
}

// LintFinding is one manifest the lint refuses.
type LintFinding struct {
	Object ObjectRef
	Reason string
}

// ApplyResult reports one Apply call.
type ApplyResult struct {
	Applied []ObjectRef
	// Changed are the objects whose resourceVersion moved.
	Changed []ObjectRef
}

// Applier builds and applies manifests with server-side apply (field manager
// "bagel-deployer", force conflicts). Apply refuses, with ErrRefused wrapped
// in a *Fail (FailApplyRefused), any object outside the allowlist: kinds
// Deployment, DaemonSet, CronJob, Service, ConfigMap, PodDisruptionBudget,
// NetworkPolicy, PriorityClass, Traefik IngressRoute and Middleware, KEDA
// ScaledObject; namespaces app, db, messaging (cluster-scoped: PriorityClass
// only). It never deletes. Before applying it drops spec.replicas from any
// Deployment targeted by a KEDA ScaledObject found in objs or live in the
// Deployment's namespace.
type Applier interface {
	Build(ctx context.Context, spec BuildSpec) (Objects, error)
	// Lint refuses a one-pod-per-node workload (required podAntiAffinity or
	// a DoNotSchedule topologySpreadConstraint on kubernetes.io/hostname)
	// whose rolling update has maxSurge > 0. An empty result passes.
	Lint(objs Objects) []LintFinding
	Apply(ctx context.Context, objs Objects) (ApplyResult, error)
}

// ---- Cluster: watch ----

// WorkloadRef names one workload.
type WorkloadRef struct {
	Kind      string // Deployment, DaemonSet, CronJob
	Namespace Namespace
	Name      string
}

// Unsettled is one workload mid-rollout or unhealthy.
type Unsettled struct {
	Workload WorkloadRef
	Reason   string
}

// LiveImage is one container's running image reference.
type LiveImage struct {
	Workload  WorkloadRef
	Container string
	Image     string // as in the pod spec: repo:tag@sha256:...
}

// Pins maps an image to the digest its pods must run.
type Pins map[deploy.ImageName]deploy.Digest

// RolloutSpec is one workload to wait on.
type RolloutSpec struct {
	Workload WorkloadRef
	Pins     Pins
}

// ProgressFunc receives rollout progress as the Item the stage shows; Key
// and Label are the stage's to set.
type ProgressFunc func(deploy.Item)

// Mismatch is one pod not running its pinned digest.
type Mismatch struct {
	Workload  WorkloadRef
	Pod       string
	Container string
	Want      deploy.Digest
	Got       deploy.Digest
}

// NATSServer is one NATS server's reload state.
type NATSServer struct {
	Pod          string
	Node         string
	ConfigLoaded time.Time
}

// Watcher observes the cluster. Every wait is bounded by ctx; the stage sets
// the deadline.
type Watcher interface {
	// Reachable errors when the API server does not answer.
	Reachable(ctx context.Context) error
	// Settled returns the workloads among refs that are not fully rolled out
	// and available. An empty result means settled.
	Settled(ctx context.Context, refs []WorkloadRef) ([]Unsettled, error)
	// LiveImages lists the images every container of refs runs.
	LiveImages(ctx context.Context, refs []WorkloadRef) ([]LiveImage, error)
	// WaitRollout blocks until the workload's new generation is fully
	// updated, ready and available, reporting progress (updated and ready /
	// replicas, plus per-node pod dots) on every change. It fast-fails with
	// a *Fail: FailUnschedulable for a FailedScheduling event older than
	// Config.FailedSchedulingAfter, FailCrashLoop for a new-ReplicaSet pod at
	// Config.RestartLimit restarts, FailImagePull for ImagePullBackOff or
	// ErrImagePull, FailTimeout at ctx deadline. Fail.LogTail carries the
	// pod events plus the last Config.LogTailLines lines of the failing
	// container. CronJobs return at once (nothing to roll).
	WaitRollout(ctx context.Context, spec RolloutSpec, progress ProgressFunc) error
	// VerifyImageIDs compares every pod's containerStatuses imageID digest
	// with the pins.
	VerifyImageIDs(ctx context.Context, refs []WorkloadRef, pins Pins) ([]Mismatch, error)
	// NATSServers reads /varz config_load_time from every NATS server pod
	// (hub Deployment and leaf DaemonSet in namespace messaging, port 8222).
	NATSServers(ctx context.Context) ([]NATSServer, error)
	// Probe GETs url without following redirects and returns the status code.
	Probe(ctx context.Context, url URL) (int, error)
}

// ---- State ----

// Revision is a KV revision for compare-and-set.
type Revision uint64

// Lock is the held cluster-wide deploy lock.
type Lock struct {
	Owner    deploy.RunID
	Revision Revision
}

// Store persists runs in the DEPLOY_RUNS JetStream KV bucket on the hub.
type Store interface {
	// Get returns a run and its revision; ErrNotFound when absent.
	Get(ctx context.Context, id deploy.RunID) (deploy.Run, Revision, error)
	// Put writes a run. rev 0 creates (ErrConflict if it exists); otherwise
	// it must match the stored revision (ErrConflict if not). Put also
	// maintains the active-run pointer: set while run.State is not terminal,
	// cleared when it is. Runs beyond deploy.KeepRuns are pruned oldest first.
	Put(ctx context.Context, run *deploy.Run, rev Revision) (Revision, error)
	// Active returns the non-terminal run, if any.
	Active(ctx context.Context) (deploy.Run, Revision, bool, error)
	// List returns up to limit runs, newest first.
	List(ctx context.Context, limit int) ([]deploy.RunSummary, error)

	// AcquireLock takes the cluster-wide lock for owner; ErrLockHeld when
	// another owner's lock has not expired (Config.LockTTL after its last
	// heartbeat). Re-acquiring a lock owner already holds succeeds.
	AcquireLock(ctx context.Context, owner deploy.RunID) (Lock, error)
	// Heartbeat extends the lock; ErrLockHeld if it was lost.
	Heartbeat(ctx context.Context, lock Lock) (Lock, error)
	ReleaseLock(ctx context.Context, lock Lock) error
	// LockOwner returns the current unexpired lock owner, if any.
	LockOwner(ctx context.Context) (deploy.RunID, bool, error)
}

// Events publishes run snapshots on deploy.EventsSubject(run.ID).
type Events interface {
	Publish(ctx context.Context, run *deploy.Run) error
}

// Authorizer resolves an actor through the users service
// (bagel.rpc.admin.user.auth.check, role read from the staff table) and
// refuses with ErrForbidden anything but active staff with role owner. It
// fails closed: any error resolving the actor is ErrForbidden-wrapped.
type Authorizer interface {
	RequireOwner(ctx context.Context, actorID string) (deploy.Actor, error)
}
