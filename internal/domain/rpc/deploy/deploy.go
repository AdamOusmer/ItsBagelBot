// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package deploy

import (
	"time"

	"ItsBagelBot/internal/domain/rpc"
)

const (
	Prefix       = "bagel.rpc.admin.deploy"
	EventsPrefix = "bagel.deploy.events"
	KVBucket     = "DEPLOY_RUNS"
	KeepRuns     = 50

	VerbPlan    = "plan"
	VerbStart   = "start"
	VerbGet     = "get"
	VerbList    = "list"
	VerbResume  = "resume"
	VerbCancel  = "cancel"
	VerbApprove = "approve"
)

func Subject(verb string) string { return Prefix + "." + verb }

func EventsSubject(id RunID) string { return EventsPrefix + "." + string(id) }

type (
	RunID     string
	SHA       string
	Version   string
	ImageName string
	Tag       string
	Digest    string
)

type RunKind string

const (
	KindRelease  RunKind = "release"
	KindHotfix   RunKind = "hotfix"
	KindBump     RunKind = "bump"
	KindRollback RunKind = "rollback"
	KindReapply  RunKind = "reapply"
)

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

var stagesByKind = map[RunKind][]StageID{
	KindRelease:  {StagePreflight, StageMergePRs, StageChangelog, StageTag, StageRelease, StageBuild, StageDigests, StagePinPR, StageACL, StageRollout, StageVerify},
	KindHotfix:   {StagePreflight, StageMergePRs, StageChangelog, StageTag, StageRelease, StageBuild, StageDigests, StagePinPR, StageACL, StageRollout, StageVerify},
	KindBump:     {StagePreflight, StageMergePRs, StageBuild, StageDigests, StagePinPR, StageACL, StageRollout, StageVerify},
	KindRollback: {StagePreflight, StagePinPR, StageACL, StageRollout, StageVerify},
	KindReapply:  {StagePreflight, StageACL, StageRollout, StageVerify},
}

func StagesFor(kind RunKind) []StageID {
	return append([]StageID(nil), stagesByKind[kind]...)
}

func Kinds() []RunKind {
	return []RunKind{KindRelease, KindHotfix, KindBump, KindRollback, KindReapply}
}

type StageState string

const (
	StatePending   StageState = "pending"
	StateRunning   StageState = "running"
	StateWaiting   StageState = "waiting"
	StateSucceeded StageState = "succeeded"
	StateFailed    StageState = "failed"
	StateSkipped   StageState = "skipped"
	StateCancelled StageState = "cancelled"
)

type RunState string

const (
	RunRunning      RunState = "running"
	RunWaiting      RunState = "waiting"
	RunSucceeded    RunState = "succeeded"
	RunFailed       RunState = "failed"
	RunCancelled    RunState = "cancelled"
	RunVerifyFailed RunState = "verify_failed"
)

func (s RunState) Terminal() bool {
	switch s {
	case RunSucceeded, RunFailed, RunCancelled, RunVerifyFailed:
		return true
	}
	return false
}

type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

type PodPhase string

const (
	PodOld     PodPhase = "old"
	PodNew     PodPhase = "new"
	PodPending PodPhase = "pending"
	PodFailing PodPhase = "failing"
)

type NodePod struct {
	Node  string   `json:"node"`
	Pod   string   `json:"pod"`
	Phase PodPhase `json:"phase"`
}

type Item struct {
	Key      string     `json:"key"`
	Label    string     `json:"label"`
	State    StageState `json:"state"`
	Progress Progress   `json:"progress"`
	Nodes    []NodePod  `json:"nodes,omitempty"`
	Detail   string     `json:"detail,omitempty"`
	URL      string     `json:"url,omitempty"`
}

type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

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
	FailClaimsPush         FailureCode = "claims_push_failed"
)

type Action string

const (
	ActionResume   Action = "resume"
	ActionRerun    Action = "rerun_failed_jobs"
	ActionRollback Action = "rollback"
	ActionCancel   Action = "cancel"
)

type Failure struct {
	Code    FailureCode `json:"code"`
	Message string      `json:"message"`
	LogTail []string    `json:"log_tail,omitempty"`
	Actions []Action    `json:"actions,omitempty"`
}

type Stage struct {
	ID            StageID    `json:"id"`
	State         StageState `json:"state"`
	Progress      Progress   `json:"progress"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	Items         []Item     `json:"items,omitempty"`
	Links         []Link     `json:"links,omitempty"`
	Failure       *Failure   `json:"failure,omitempty"`
	Waiting       string     `json:"waiting,omitempty"`
	NeedsApproval string     `json:"needs_approval,omitempty"`
	Attempts      int        `json:"attempts,omitempty"`
}

type ImagePin struct {
	Tag    Tag    `json:"tag"`
	Digest Digest `json:"digest"`
}

type Outputs struct {
	LiveSHA     SHA     `json:"live_sha,omitempty"`
	LiveVersion Version `json:"live_version,omitempty"`

	MergedPRs       []int  `json:"merged_prs,omitempty"`
	ChangelogPR     int    `json:"changelog_pr,omitempty"`
	ChangelogSHA    SHA    `json:"changelog_sha,omitempty"`
	TagSHA          SHA    `json:"tag_sha,omitempty"`
	ReleaseURL      string `json:"release_url,omitempty"`
	BuildRunID      int64  `json:"build_run_id,omitempty"`
	BuildRunURL     string `json:"build_run_url,omitempty"`
	BuildRunAttempt int    `json:"build_run_attempt,omitempty"`

	Digests  map[ImageName]ImagePin `json:"digests,omitempty"`
	Services []string               `json:"services,omitempty"`

	PinPR  int `json:"pin_pr,omitempty"`
	PinSHA SHA `json:"pin_sha,omitempty"`

	MessagingChanged bool       `json:"messaging_changed"`
	ACLAppliedAt     *time.Time `json:"acl_applied_at,omitempty"`
	ACLPushed        bool       `json:"acl_pushed,omitempty"`
}

type Actor struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}

type ChangelogEntry struct {
	Title      map[string]string   `json:"title"`
	Highlights map[string][]string `json:"highlights"`
	Date       string              `json:"date,omitempty"`
}

type Run struct {
	ID        RunID   `json:"id"`
	Seq       uint64  `json:"seq"`
	Kind      RunKind `json:"kind"`
	Version   Version `json:"version,omitempty"`
	TargetSHA SHA     `json:"target_sha,omitempty"`
	PRs       []int   `json:"prs,omitempty"`

	Changelog  *ChangelogEntry `json:"changelog,omitempty"`
	RollbackTo Version         `json:"rollback_to,omitempty"`
	Services   []string        `json:"services,omitempty"`

	Actor   Actor    `json:"actor"`
	State   RunState `json:"state"`
	Stages  []Stage  `json:"stages"`
	Outputs Outputs  `json:"outputs"`
	Failure *Failure `json:"failure,omitempty"`

	CancelRequested bool `json:"cancel_requested,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

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

type CheckState string

const (
	ChecksNone    CheckState = "none"
	ChecksPending CheckState = "pending"
	ChecksSuccess CheckState = "success"
	ChecksFailure CheckState = "failure"
)

type PRInfo struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Author    string     `json:"author"`
	URL       string     `json:"url"`
	HeadSHA   SHA        `json:"head_sha"`
	Draft     bool       `json:"draft"`
	Checks    CheckState `json:"checks"`
	CodeScene CheckState `json:"codescene"`
	Mergeable bool       `json:"mergeable"`
	Behind    bool       `json:"behind"`
}

type Commit struct {
	SHA    SHA    `json:"sha"`
	Title  string `json:"title"`
	Author string `json:"author"`
	PR     int    `json:"pr,omitempty"`
	URL    string `json:"url,omitempty"`
}

type DriftItem struct {
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
	Container string `json:"container"`
	Pinned    string `json:"pinned"`
	Live      string `json:"live"`
}

type ReleaseInfo struct {
	Version     Version   `json:"version"`
	SHA         SHA       `json:"sha"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"published_at"`
}

type Plan struct {
	LiveVersion Version       `json:"live_version,omitempty"`
	LiveSHA     SHA           `json:"live_sha,omitempty"`
	LastTag     Version       `json:"last_tag,omitempty"`
	NextVersion Version       `json:"next_version,omitempty"`
	MainSHA     SHA           `json:"main_sha,omitempty"`
	InSync      bool          `json:"in_sync"`
	Drift       []DriftItem   `json:"drift,omitempty"`
	Commits     []Commit      `json:"commits,omitempty"`
	PRs         []PRInfo      `json:"prs,omitempty"`
	Releases    []ReleaseInfo `json:"releases,omitempty"`
	Services    []string      `json:"services,omitempty"`
	ActiveRunID RunID         `json:"active_run_id,omitempty"`
}

type PlanRequest struct {
	ActorID    string  `json:"actor_id"`
	Kind       RunKind `json:"kind,omitempty"`
	RollbackTo Version `json:"rollback_to,omitempty"`
}

type StartRequest struct {
	ActorID    string          `json:"actor_id"`
	Kind       RunKind         `json:"kind"`
	Version    Version         `json:"version,omitempty"`
	TargetSHA  SHA             `json:"target_sha,omitempty"`
	PRs        []int           `json:"prs,omitempty"`
	Changelog  *ChangelogEntry `json:"changelog,omitempty"`
	RollbackTo Version         `json:"rollback_to,omitempty"`
	Services   []string        `json:"services,omitempty"`
}

type RunRequest struct {
	ActorID string  `json:"actor_id"`
	RunID   RunID   `json:"run_id"`
	Stage   StageID `json:"stage,omitempty"`
	Rerun   bool    `json:"rerun,omitempty"`
}

type ListRequest struct {
	ActorID string `json:"actor_id"`
	Limit   int    `json:"limit,omitempty"`
}

type PlanReply struct {
	Plan *Plan `json:"plan,omitempty"`
	rpc.Refusal
}

type RunReply struct {
	Run *Run `json:"run,omitempty"`
	rpc.Refusal
}

type ListReply struct {
	Runs        []RunSummary `json:"runs"`
	ActiveRunID RunID        `json:"active_run_id,omitempty"`
	rpc.Refusal
}

func (r *Run) Summary() RunSummary {
	return RunSummary{
		ID: r.ID, Kind: r.Kind, Version: r.Version, TargetSHA: r.TargetSHA,
		State: r.State, CurrentStage: r.CurrentStage(), Actor: r.Actor,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func (r *Run) CurrentStage() StageID {
	for _, s := range r.Stages {
		if !s.State.finished() {
			return s.ID
		}
	}
	return ""
}

func (r *Run) Stage(id StageID) *Stage {
	for i := range r.Stages {
		if r.Stages[i].ID == id {
			return &r.Stages[i]
		}
	}
	return nil
}

func (s StageState) finished() bool { return s == StateSucceeded || s == StateSkipped }
