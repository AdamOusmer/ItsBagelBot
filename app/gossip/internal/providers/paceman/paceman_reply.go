// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package paceman

import (
	"fmt"
	"strings"
	"time"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

// This file holds the reply-shaping helpers that turn an upstream response
// into a gossip reply for paceman's four endpoints (session, nethers,
// lastfort, personal_best), which live in paceman_endpoints.go alongside
// their error-reply counterparts.

// --- reply shaping -----------------------------------------------------------------

// buildSessionReply combines the two session upstream calls into one reply.
// Empty tracks NetherCount alone: a run cannot reach any later split without
// first entering a nether, so a zero nether count is the one check that means
// "nothing tracked this window" for every field below it.
func buildSessionReply(account string, stats sessionStatsResponse, nethers sessionNethersResponse) gossiprpc.PacemanSessionReply {
	return gossiprpc.PacemanSessionReply{
		Player:          account,
		NetherCount:     stats.Nether.Count,
		Nether:          stats.Nether.Avg,
		Bastion:         stats.Bastion.Avg,
		Fortress:        stats.Fortress.Avg,
		FirstStructure:  stats.FirstStructure.Avg,
		SecondStructure: stats.SecondStructure.Avg,
		FirstPortal:     stats.FirstPortal.Avg,
		Stronghold:      stats.Stronghold.Avg,
		End:             stats.End.Avg,
		Finish:          stats.Finish.Avg,
		NPH:             nethers.RNPH,
		Empty:           stats.Nether.Count == 0,
	}
}

// unixTime converts a PaceMan fractional-second epoch value to a time.Time at
// whole-second resolution; sub-second precision does not change either the
// "m:ss" split rendering or the "how long ago" rounding these replies do.
func unixTime(sec float64) time.Time { return time.Unix(int64(sec), 0) }

// formatMMSS renders an elapsed duration the way PaceMan's own "avg" fields
// do ("m:ss"), so a computed split reads identically to an upstream one.
func formatMMSS(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int64(seconds)
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

// splitDuration renders one run split as elapsed time since the run started,
// or "" when the run never reached it (module.go turns that into an em dash).
func splitDuration(start float64, split *float64) string {
	if split == nil {
		return ""
	}
	return formatMMSS(*split - start)
}

// buildLastFortReply shapes one recentTimestamp row into the reply, deriving
// each split's elapsed time from its wall-clock timestamp and the run's start.
func buildLastFortReply(account string, run recentTimestamp) gossiprpc.PacemanLastFortReply {
	return gossiprpc.PacemanLastFortReply{
		Player:      account,
		Nether:      splitDuration(run.Start, run.Nether),
		Bastion:     splitDuration(run.Start, run.Bastion),
		Fortress:    splitDuration(run.Start, run.Fortress),
		FirstPortal: splitDuration(run.Start, run.FirstPortal),
		Stronghold:  splitDuration(run.Start, run.Stronghold),
		End:         splitDuration(run.Start, run.End),
		Finish:      splitDuration(run.Start, run.Finish),
		AgoSeconds:  int64(time.Since(unixTime(run.Start)).Seconds()),
	}
}

// normalizePBWindow maps a request's raw TimeWindow onto the four windows
// PaceMan precomputes. Anything unrecognized (including "", the bare-name
// form's zero value) falls through to "all-time" — the same "no window typed
// means all-time" default sesame's argument parsing applies before the call
// even reaches here, so this is a defensive second normalization, not the
// primary one.
func normalizePBWindow(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "daily":
		return "daily"
	case "weekly":
		return "weekly"
	case "monthly":
		return "monthly"
	default:
		return "all-time"
	}
}

// selectPB picks the one pbCompletion the normalized window names out of the
// four PaceMan returned in a single call.
func selectPB(resp userPBsResponse, window string) *pbCompletion {
	switch window {
	case "daily":
		return resp.PBs.Daily
	case "weekly":
		return resp.PBs.Weekly
	case "monthly":
		return resp.PBs.Monthly
	default:
		return resp.PBs.AllTime
	}
}

// pacemanFormatTime renders a completion time in milliseconds the way MCSR
// speedrunning clients display a run (minutes:seconds.milliseconds),
// matching the mcsr provider's own mcsrFormatTime so a PaceMan-sourced time
// and an MCSR-Ranked-sourced time read identically in chat.
func pacemanFormatTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	minutes := ms / 60000
	seconds := (ms % 60000) / 1000
	millis := ms % 1000
	return fmt.Sprintf("%d:%02d.%03d", minutes, seconds, millis)
}

// buildPersonalBestReply shapes the cached four-window response down to the
// one window a !pb call asked for.
func buildPersonalBestReply(account, window string, resp userPBsResponse) gossiprpc.PacemanPersonalBestReply {
	pb := selectPB(resp, window)
	if pb == nil {
		return gossiprpc.PacemanPersonalBestReply{Player: account, Window: window, Empty: true}
	}
	return gossiprpc.PacemanPersonalBestReply{Player: account, Window: window, Time: pacemanFormatTime(pb.Time)}
}

// --- endpoints ------------------------------------------------------------------
