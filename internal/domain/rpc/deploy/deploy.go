// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package deploy holds the wire contract between the admin console's Deploys
// page and the deployer service: the verbs under Prefix, the Run snapshot the
// deployer publishes on EventsPrefix after every state change, and the plan
// the page renders before a run starts.
//
// The Run snapshot is always the whole run, never a delta. A page that missed
// an event (reconnect, tab asleep) is correct again on the next one, and the
// Seq field lets it drop a snapshot that arrives after a newer one instead of
// rendering progress backwards.
package deploy

import (
	"time"

	"ItsBagelBot/internal/domain/rpc"
)

// Subjects. Prefix sits under bagel.rpc.admin so the console's existing admin
// NATS user can reach it through the DEPLOYER_RPC export without a new
// publish permission shape; the events subject is outside the rpc tree
// because it is fan-out, not request/reply.
const (
	Prefix       = "bagel.rpc.admin.deploy"
	EventsPrefix = "bagel.deploy.events"
	// KVBucket holds the active run and the last KeepRuns runs on the hub.
	KVBucket = "DEPLOY_RUNS"
	// KeepRuns bounds the history the list verb can return.
	KeepRuns = 50

	VerbPlan    = "plan"
	VerbStart   = "start"
	VerbGet     = "get"
	VerbList    = "list"
	VerbResume  = "resume"
	VerbCancel  = "cancel"
	VerbApprove = "approve"
)

// Subject returns the full subject of one verb.
func Subject(verb string) string { return Prefix + "." + verb }

// EventsSubject returns the subject the snapshots of one run are published on.
func EventsSubject(id RunID) string { return EventsPrefix + "." + string(id) }

// Named string types. Defined rather than bare strings so a SHA cannot be
// passed where a tag belongs: every one of these is a plausible string in the
// other's position, and the deployer moves them between GitHub, the registry
// and the cluster constantly.
type (
	RunID     string
	SHA       string
	Version   string // vX.Y.Z-beta
	ImageName string // path under the image repo, e.g. "commands", "warp"
	Tag       string // image tag, e.g. "v0.2.2-beta" or "main-1758000000-1a2b3c4d5e6f"
	Digest    string // "sha256:<hex>"
)

// RunKind is what a run does. See StagesFor for the stages each one runs.
type RunKind string

const (
	// KindRelease cuts a new version: changelog, tag, release, build, pin, roll.
	KindRelease RunKind = "release"
	// KindHotfix keeps the version and force-moves its tag to a later commit.
	KindHotfix RunKind = "hotfix"
	// KindBump pins the main-<ts>-<sha12> images a main push built, for the
	// services whose image changed. No tag, no release.
	KindBump RunKind = "bump"
	// KindRollback pins an earlier release's images. No build.
	KindRollback RunKind = "rollback"
	// KindReapply rolls out the pins already on main. No GitHub writes.
	KindReapply RunKind = "reapply"
)

// StageID names one step of the train. The constants are declared in train
// order; StagesFor preserves it.
type StageID string

const (
	StagePreflight StageID = "preflight"
	StageMergePRs  StageID = "merge_prs"
	StageChangelog StageID = "changelog"
	StageTag       StageID = "tag"
	StageRelease   StageID = "release"
	StageBuild     StageID = "build"
	StageDigests   StageID = "digests"
	StagePinPR     StageID = "pin_pr"
	StageACL       StageID = "acl"
	StageRollout   StageID = "rollout"
	StageVerify    StageID = "verify"
)

// stagesByKind is the train per kind. merge_prs is listed wherever PRs may be
// merged first and skips itself when the run names none; acl skips itself
// when deploy/messaging did not change.
var stagesByKind = map[RunKind][]StageID{
	KindRelease:  {StagePreflight, StageMergePRs, StageChangelog, StageTag, StageRelease, StageBuild, StageDigests, StagePinPR, StageACL, StageRollout, StageVerify},
	KindHotfix:   {StagePreflight, StageMergePRs, StageChangelog, StageTag, StageRelease, StageBuild, StageDigests, StagePinPR, StageACL, StageRollout, StageVerify},
	KindBump:     {StagePreflight, StageMergePRs, StageBuild, StageDigests, StagePinPR, StageACL, StageRollout, StageVerify},
	KindRollback: {StagePreflight, StagePinPR, StageACL, StageRollout, StageVerify},
	KindReapply:  {StagePreflight, StageACL, StageRollout, StageVerify},
}

// StagesFor returns the ordered stages a run of this kind executes, or nil
// for an unknown kind. The slice is a copy.
func StagesFor(kind RunKind) []StageID {
	return append([]StageID(nil), stagesByKind[kind]...)
}

// Kinds lists every run kind, for validation and the console's picker.
func Kinds() []RunKind {
	return []RunKind{KindRelease, KindHotfix, KindBump, KindRollback, KindReapply}
}

// StageState is the state of one stage, and of one Item inside a stage.
type StageState string

const (
	StatePending   StageState = "pending"
	StateRunning   StageState = "running"
	StateWaiting   StageState = "waiting" // blocked on approval or on something outside the deployer (queued build, checks)
	StateSucceeded StageState = "succeeded"
	StateFailed    StageState = "failed"
	StateSkipped   StageState = "skipped"
	StateCancelled StageState = "cancelled"
)

// RunState is the state of a whole run.
type RunState string

const (
	RunRunning   RunState = "running"
	RunWaiting   RunState = "waiting" // a stage awaits the approve verb
	RunSucceeded RunState = "succeeded"
	RunFailed    RunState = "failed" // resumable from the failed stage
	RunCancelled RunState = "cancelled"
	// RunVerifyFailed means everything rolled out and verify did not pass.
	// Deployed, not rolled back: rollback is offered, never automatic.
	RunVerifyFailed RunState = "verify_failed"
)

// Terminal reports whether no further stage will run without a resume.
func (s RunState) Terminal() bool {
	switch s {
	case RunSucceeded, RunFailed, RunCancelled, RunVerifyFailed:
		return true
	}
	return false
}

// Progress is a done/total pair; Total zero means indeterminate.
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// PodPhase is one pod's place in a rollout, drawn as a dot per node.
type PodPhase string

const (
	PodOld     PodPhase = "old"     // running the previous ReplicaSet
	PodNew     PodPhase = "new"     // new ReplicaSet, ready
	PodPending PodPhase = "pending" // new ReplicaSet, not ready yet
	PodFailing PodPhase = "failing" // new ReplicaSet, crash-looping, image pull error or unschedulable
)

// NodePod is one pod dot.
type NodePod struct {
	Node  string   `json:"node"`
	Pod   string   `json:"pod"`
	Phase PodPhase `json:"phase"`
}

// Item is one row inside a stage: a PR in merge_prs, a job in build, an image
// in digests, a service in rollout, a NATS server in acl, a probe in verify.
// Key is stable for the row's lifetime so the page can animate it in place.
type Item struct {
	Key      string     `json:"key"`
	Label    string     `json:"label"`
	State    StageState `json:"state"`
	Progress Progress   `json:"progress"`
	Nodes    []NodePod  `json:"nodes,omitempty"`
	Detail   string     `json:"detail,omitempty"`
	URL      string     `json:"url,omitempty"`
}

// Link is an external reference a stage produced (a PR, a workflow run, a release).
type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// FailureCode says what kind of failure stopped a stage. The console picks
// its copy from it; Message carries the specifics.
type FailureCode string

const (
	FailLockHeld           FailureCode = "lock_held"
	FailClusterUnreachable FailureCode = "cluster_unreachable"
	FailNotSettled         FailureCode = "not_settled"
	FailChecksFailed       FailureCode = "checks_failed"
	FailLintRefused        FailureCode = "lint_refused"
	FailNotMergeable       FailureCode = "not_mergeable"
	FailBehindLimit        FailureCode = "behind_limit"
	FailBuildFailed        FailureCode = "build_failed"
	FailDigestRefused      FailureCode = "digest_refused"
	FailApplyRefused       FailureCode = "apply_refused"
	FailApplyFailed        FailureCode = "apply_failed"
	FailUnschedulable      FailureCode = "unschedulable"
	FailCrashLoop          FailureCode = "crash_loop"
	FailImagePull          FailureCode = "image_pull"
	FailTimeout            FailureCode = "timeout"
	FailVerify             FailureCode = "verify_failed"
	FailGitHub             FailureCode = "github"
	FailKube               FailureCode = "kube"
	FailInternal           FailureCode = "internal"
)

// Action is a verb the console may offer on a failure.
type Action string

const (
	ActionResume   Action = "resume"
	ActionRerun    Action = "rerun_failed_jobs" // resume with Rerun set
	ActionRollback Action = "rollback"          // start a KindRollback run
	ActionCancel   Action = "cancel"
)

// Failure is why a stage or run stopped. LogTail holds at most the last 50
// lines of the relevant job or container log.
type Failure struct {
	Code    FailureCode `json:"code"`
	Message string      `json:"message"`
	LogTail []string    `json:"log_tail,omitempty"`
	Actions []Action    `json:"actions,omitempty"`
}

// Stage is one stage's live state inside a Run.
type Stage struct {
	ID        StageID    `json:"id"`
	State     StageState `json:"state"`
	Progress  Progress   `json:"progress"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Items     []Item     `json:"items,omitempty"`
	Links     []Link     `json:"links,omitempty"`
	Failure   *Failure   `json:"failure,omitempty"`
	// Waiting explains a StateWaiting stage that needs no operator action
	// ("queued behind run 1234", "waiting for required checks").
	Waiting string `json:"waiting,omitempty"`
	// NeedsApproval, when set, is the question the approve verb answers.
	NeedsApproval string `json:"needs_approval,omitempty"`
	// Attempts counts how many times the stage body started (resumes included).
	Attempts int `json:"attempts,omitempty"`
}

// ImagePin is one image's pinned reference: written as tag@digest.
type ImagePin struct {
	Tag    Tag    `json:"tag"`
	Digest Digest `json:"digest"`
}

// Outputs are the facts earlier stages hand to later ones. They are
// persisted with the run so a resumed run never recomputes them from a world
// that has since moved (main advanced, a newer build finished).
type Outputs struct {
	// LiveSHA is the pin commit that was live on the cluster when the run
	// started; acl diffs deploy/messaging against it.
	LiveSHA     SHA     `json:"live_sha,omitempty"`
	LiveVersion Version `json:"live_version,omitempty"`

	MergedPRs    []int  `json:"merged_prs,omitempty"`
	ChangelogPR  int    `json:"changelog_pr,omitempty"`
	ChangelogSHA SHA    `json:"changelog_sha,omitempty"` // merge commit, the tag target
	TagSHA       SHA    `json:"tag_sha,omitempty"`       // commit the tag points at
	ReleaseURL   string `json:"release_url,omitempty"`
	BuildRunID   int64  `json:"build_run_id,omitempty"`
	BuildRunURL  string `json:"build_run_url,omitempty"`
	// BuildRunAttempt is the lowest run_attempt the build stage accepts. A
	// rerun keeps the run id, and GitHub reports the old attempt's failed
	// conclusion until the new attempt is queued, so without this floor a
	// resumed stage reads the stale failure and fails again at once.
	BuildRunAttempt int `json:"build_run_attempt,omitempty"`

	Digests map[ImageName]ImagePin `json:"digests,omitempty"`
	// Services are the rollout units whose image changed (bump) or all of
	// them (every other kind).
	Services []string `json:"services,omitempty"`

	PinPR  int `json:"pin_pr,omitempty"`
	PinSHA SHA `json:"pin_sha,omitempty"` // pin PR merge commit; rollout reads manifests here

	MessagingChanged bool       `json:"messaging_changed"`
	ACLAppliedAt     *time.Time `json:"acl_applied_at,omitempty"`
}

// Actor is the verified operator. Login comes from the users service staff
// row, never from the request.
type Actor struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}

// ChangelogEntry is the operator-written part of
// web/marketing/src/content/changelog/<version>.json. The deployer adds tag
// "beta", version, date (today UTC when empty) and the github release URL.
// Maps are keyed by locale; "en" is required.
type ChangelogEntry struct {
	Title      map[string]string   `json:"title"`
	Highlights map[string][]string `json:"highlights"`
	Date       string              `json:"date,omitempty"` // YYYY-MM-DD
}

// Run is the full state of one run. It is the KV value and the event payload.
type Run struct {
	ID        RunID   `json:"id"`
	Seq       uint64  `json:"seq"`
	Kind      RunKind `json:"kind"`
	Version   Version `json:"version,omitempty"`
	TargetSHA SHA     `json:"target_sha,omitempty"`
	PRs       []int   `json:"prs,omitempty"`

	Changelog  *ChangelogEntry `json:"changelog,omitempty"`
	RollbackTo Version         `json:"rollback_to,omitempty"`
	// Services narrows a bump to these rollout units; empty means every
	// service whose image changed.
	Services []string `json:"services,omitempty"`

	Actor   Actor    `json:"actor"`
	State   RunState `json:"state"`
	Stages  []Stage  `json:"stages"`
	Outputs Outputs  `json:"outputs"`
	Failure *Failure `json:"failure,omitempty"`

	// CancelRequested is set by the cancel verb; the running stage honours
	// it at its next safe point.
	CancelRequested bool `json:"cancel_requested,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RunSummary is one row of the list verb.
type RunSummary struct {
	ID           RunID     `json:"id"`
	Kind         RunKind   `json:"kind"`
	Version      Version   `json:"version,omitempty"`
	TargetSHA    SHA       `json:"target_sha,omitempty"`
	State        RunState  `json:"state"`
	CurrentStage StageID   `json:"current_stage,omitempty"`
	Actor        Actor     `json:"actor"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CheckState aggregates GitHub check runs and statuses.
type CheckState string

const (
	ChecksNone    CheckState = "none"
	ChecksPending CheckState = "pending"
	ChecksSuccess CheckState = "success"
	ChecksFailure CheckState = "failure"
)

// PRInfo is one open PR offered for merge_prs.
type PRInfo struct {
	Number  int        `json:"number"`
	Title   string     `json:"title"`
	Author  string     `json:"author"`
	URL     string     `json:"url"`
	HeadSHA SHA        `json:"head_sha"`
	Draft   bool       `json:"draft"`
	Checks  CheckState `json:"checks"`
	// CodeScene is the "CodeScene Code Health Review (main)" check alone.
	CodeScene CheckState `json:"codescene"`
	Mergeable bool       `json:"mergeable"`
	Behind    bool       `json:"behind"`
}

// Commit is one commit on main not yet in the live version.
type Commit struct {
	SHA    SHA    `json:"sha"`
	Title  string `json:"title"`
	Author string `json:"author"`
	PR     int    `json:"pr,omitempty"`
	URL    string `json:"url,omitempty"`
}

// DriftItem is one workload whose live image differs from the pin on main.
type DriftItem struct {
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
	Container string `json:"container"`
	Pinned    string `json:"pinned"` // image reference on main
	Live      string `json:"live"`   // image reference running
}

// ReleaseInfo is an earlier release offered as a rollback target.
type ReleaseInfo struct {
	Version     Version   `json:"version"`
	SHA         SHA       `json:"sha"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"published_at"`
}

// Plan is what the page shows before a run: where the cluster is, what main
// has, and what each kind would do.
type Plan struct {
	LiveVersion Version `json:"live_version,omitempty"`
	LiveSHA     SHA     `json:"live_sha,omitempty"`
	LastTag     Version `json:"last_tag,omitempty"`
	NextVersion Version `json:"next_version,omitempty"`
	MainSHA     SHA     `json:"main_sha,omitempty"`
	// InSync is true when every managed workload runs exactly its pin on main.
	InSync   bool          `json:"in_sync"`
	Drift    []DriftItem   `json:"drift,omitempty"`
	Commits  []Commit      `json:"commits,omitempty"`
	PRs      []PRInfo      `json:"prs,omitempty"`
	Releases []ReleaseInfo `json:"releases,omitempty"`
	// Services are the rollout units in rollout order; for a bump plan, only
	// those whose image changed on main.
	Services    []string `json:"services,omitempty"`
	ActiveRunID RunID    `json:"active_run_id,omitempty"`
}

// Requests. ActorID is an identity claim only: the deployer resolves the
// actor's role through the users service and refuses anything below owner.

type PlanRequest struct {
	ActorID string  `json:"actor_id"`
	Kind    RunKind `json:"kind,omitempty"`
	// RollbackTo narrows a rollback plan's services to those the target
	// release built; empty plans every service.
	RollbackTo Version `json:"rollback_to,omitempty"`
}

type StartRequest struct {
	ActorID string  `json:"actor_id"`
	Kind    RunKind `json:"kind"`
	// Version: release = the new version; hotfix = the existing version whose
	// tag moves; ignored otherwise.
	Version Version `json:"version,omitempty"`
	// TargetSHA defaults to the head of main when empty.
	TargetSHA  SHA             `json:"target_sha,omitempty"`
	PRs        []int           `json:"prs,omitempty"`
	Changelog  *ChangelogEntry `json:"changelog,omitempty"`
	RollbackTo Version         `json:"rollback_to,omitempty"`
	Services   []string        `json:"services,omitempty"`
}

// RunRequest addresses one run: get, resume, cancel, approve.
type RunRequest struct {
	ActorID string `json:"actor_id"`
	RunID   RunID  `json:"run_id"`
	// Stage names the stage an approve answers; ignored by other verbs.
	Stage StageID `json:"stage,omitempty"`
	// Rerun asks resume to rerun the failed build jobs before waiting again.
	Rerun bool `json:"rerun,omitempty"`
}

type ListRequest struct {
	ActorID string `json:"actor_id"`
	Limit   int    `json:"limit,omitempty"` // 0 = KeepRuns
}

// Replies embed rpc.Refusal like every other service: `error` and `code` at
// the top level, CodeOK meaning success.

type PlanReply struct {
	Plan *Plan `json:"plan,omitempty"`
	rpc.Refusal
}

// RunReply answers start, get, resume, cancel and approve.
type RunReply struct {
	Run *Run `json:"run,omitempty"`
	rpc.Refusal
}

type ListReply struct {
	Runs []RunSummary `json:"runs"`
	// ActiveRunID is the run holding the cluster lock, if any.
	ActiveRunID RunID `json:"active_run_id,omitempty"`
	rpc.Refusal
}

// Summary projects a run onto its list row.
func (r *Run) Summary() RunSummary {
	return RunSummary{
		ID: r.ID, Kind: r.Kind, Version: r.Version, TargetSHA: r.TargetSHA,
		State: r.State, CurrentStage: r.CurrentStage(), Actor: r.Actor,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// CurrentStage is the first stage that has not finished (succeeded or
// skipped), or empty when all have.
func (r *Run) CurrentStage() StageID {
	for _, s := range r.Stages {
		if !s.State.finished() {
			return s.ID
		}
	}
	return ""
}

// Stage returns a pointer to the named stage inside the run, or nil.
func (r *Run) Stage(id StageID) *Stage {
	for i := range r.Stages {
		if r.Stages[i].ID == id {
			return &r.Stages[i]
		}
	}
	return nil
}

func (s StageState) finished() bool { return s == StateSucceeded || s == StateSkipped }
