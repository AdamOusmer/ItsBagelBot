// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package paceman

import (
	"fmt"
	"strings"
	"time"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

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

func unixTime(sec float64) time.Time { return time.Unix(int64(sec), 0) }

func formatMMSS(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int64(seconds)
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

func splitDuration(start float64, split *float64) string {
	if split == nil {
		return ""
	}
	return formatMMSS(*split - start)
}

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

func pacemanFormatTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	minutes := ms / 60000
	seconds := (ms % 60000) / 1000
	millis := ms % 1000
	return fmt.Sprintf("%d:%02d.%03d", minutes, seconds, millis)
}

func buildPersonalBestReply(account, window string, resp userPBsResponse) gossiprpc.PacemanPersonalBestReply {
	pb := selectPB(resp, window)
	if pb == nil {
		return gossiprpc.PacemanPersonalBestReply{Player: account, Window: window, Empty: true}
	}
	return gossiprpc.PacemanPersonalBestReply{Player: account, Window: window, Time: pacemanFormatTime(pb.Time)}
}
