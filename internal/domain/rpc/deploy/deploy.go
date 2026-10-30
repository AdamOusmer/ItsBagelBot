// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package deploy

import "time"

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
	StagePromote   StageID = "promote"
)

var stagesByKind = map[RunKind][]StageID{
	KindRelease:  {StagePreflight, StageMergePRs, StageChangelog, StageTag, StageRelease, StageBuild, StageDigests, StagePinPR, StagePromote},
	KindHotfix:   {StagePreflight, StageMergePRs, StageChangelog, StageTag, StageRelease, StageBuild, StageDigests, StagePinPR, StagePromote},
	KindBump:     {StagePreflight, StageMergePRs, StageBuild, StageDigests, StagePinPR, StagePromote},
	KindRollback: {StagePreflight, StagePinPR, StagePromote},
	KindReapply:  {StagePreflight, StagePromote},
}

func StagesFor(kind RunKind) []StageID {
	return append([]StageID(nil), stagesByKind[kind]...)
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
	RunRunning   RunState = "running"
	RunSucceeded RunState = "succeeded"
	RunFailed    RunState = "failed"
)

type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

type Item struct {
	Key      string     `json:"key"`
	Label    string     `json:"label"`
	State    StageState `json:"state"`
	Progress Progress   `json:"progress"`
	Detail   string     `json:"detail,omitempty"`
	URL      string     `json:"url,omitempty"`
}

type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type FailureCode string

const (
	FailChecksFailed  FailureCode = "checks_failed"
	FailNotMergeable  FailureCode = "not_mergeable"
	FailBehindLimit   FailureCode = "behind_limit"
	FailBuildFailed   FailureCode = "build_failed"
	FailDigestRefused FailureCode = "digest_refused"
	FailGitHub        FailureCode = "github"
	FailInternal      FailureCode = "internal"
)

type Action string

const (
	ActionResume Action = "resume"
	ActionRerun  Action = "rerun_failed_jobs"
	ActionCancel Action = "cancel"
)

type Failure struct {
	Code    FailureCode `json:"code"`
	Message string      `json:"message"`
	LogTail []string    `json:"log_tail,omitempty"`
	Actions []Action    `json:"actions,omitempty"`
}

type Stage struct {
	ID        StageID    `json:"id"`
	State     StageState `json:"state"`
	Progress  Progress   `json:"progress"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Items     []Item     `json:"items,omitempty"`
	Links     []Link     `json:"links,omitempty"`
	Failure   *Failure   `json:"failure,omitempty"`
	Waiting   string     `json:"waiting,omitempty"`
	Attempts  int        `json:"attempts,omitempty"`
}

type ImagePin struct {
	Tag    Tag    `json:"tag"`
	Digest Digest `json:"digest"`
}

type Outputs struct {
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
