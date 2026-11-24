// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlaysrpc

import (
	"errors"

	"ItsBagelBot/internal/domain/overlays/feed"
	"ItsBagelBot/internal/domain/rpc"
)

const (
	FeedSnapshotSubject = "rpc.internal.overlays.feed.snapshot"
	FeedControlSubject  = "rpc.overlays.feed.control"
)

type FeedAction string

const (
	FeedPause          FeedAction = "pause"
	FeedResume         FeedAction = "resume"
	FeedMute           FeedAction = "mute"
	FeedUnmute         FeedAction = "unmute"
	FeedSkip           FeedAction = "skip"
	FeedReplay         FeedAction = "replay"
	FeedClear          FeedAction = "clear"
	FeedTest           FeedAction = "test"
	FeedSubathonAdd    FeedAction = "subathon_add"
	FeedSubathonPause  FeedAction = "subathon_pause"
	FeedSubathonReset  FeedAction = "subathon_reset"
	FeedSubathonResume FeedAction = "subathon_resume"
)

func (a FeedAction) Valid() bool {
	switch a {
	case FeedPause, FeedResume, FeedMute, FeedUnmute, FeedSkip, FeedReplay, FeedClear, FeedTest,
		FeedSubathonAdd, FeedSubathonPause, FeedSubathonReset, FeedSubathonResume:
		return true
	}
	return false
}

var (
	ErrFeedNotOwner      = errors.New("overlay feed: actor is not the channel owner")
	ErrFeedAlphaRequired = errors.New("overlay feed: alpha required")
	ErrFeedUnknownAction = errors.New("overlay feed: unknown action")
	ErrFeedInvalidArgs   = errors.New("overlay feed: invalid arguments")
	ErrFeedUnknownEvent  = errors.New("overlay feed: event not found")
	ErrFeedThrottled     = errors.New("overlay feed: too many test events")
)

// FeedRules maps the feed sentinels to refusal codes for bus.Classify.
func FeedRules() []rpc.Rule {
	return []rpc.Rule{
		rpc.Is(ErrFeedNotOwner, rpc.CodeForbidden),
		rpc.Is(ErrFeedAlphaRequired, rpc.CodeForbidden),
		rpc.Is(ErrFeedUnknownAction, rpc.CodeInvalid),
		rpc.Is(ErrFeedInvalidArgs, rpc.CodeInvalid),
		rpc.Is(ErrFeedUnknownEvent, rpc.CodeNotFound),
		rpc.Is(ErrFeedThrottled, rpc.CodeUnavailable),
	}
}

// DefaultSnapshotEvents is the tail a snapshot returns when the request names none; it matches the page's minimum event list.
const DefaultSnapshotEvents = 200

// FeedSnapshotRequest asks for the channel's feed state; Events is how many of the newest session events to return, 0 for the default.
type FeedSnapshotRequest struct {
	UserID string `json:"user_id"`
	Events int    `json:"events,omitempty"`
}

func (r FeedSnapshotRequest) Requested() string { return r.UserID }

type FeedSnapshotReply struct {
	Snapshot feed.Snapshot `json:"snapshot"`
	rpc.Refusal
}

const stateSubjectRoot = "overlays.state."

// StateSubject is the core NATS subject carrying one channel's stats, subathon and control frames; nothing is replayed.
func StateSubject(userID string) string { return stateSubjectRoot + userID }

type StateFrame struct {
	Type feed.Frame `json:"type"`
	Data any        `json:"data"`
}

// FeedControlArgs carries the arguments of the actions that take any: EventID for replay,
// Kind and Fields for test, Seconds for subathon_add.
type FeedControlArgs struct {
	EventID string       `json:"event_id,omitempty"`
	Kind    feed.Kind    `json:"kind,omitempty"`
	Fields  *feed.Fields `json:"fields,omitempty"`
	Seconds int64        `json:"seconds,omitempty"`
}

// FeedControlRequest names the channel by UserID and the user performing the action by Actor.
type FeedControlRequest struct {
	UserID string          `json:"user_id"`
	Actor  string          `json:"actor"`
	Action FeedAction      `json:"action"`
	Args   FeedControlArgs `json:"args"`
}

func (r FeedControlRequest) Requested() string { return r.UserID }

type FeedControlReply struct {
	Control  feed.Control  `json:"control"`
	Subathon feed.Subathon `json:"subathon"`
	rpc.Refusal
}
