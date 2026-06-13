package ui

import (
	"fmt"
	"strings"

	"itsbagelbot/admin/internal/rpc"
)

func JoinNodes(nodes []string) string {
	if len(nodes) == 0 {
		return "none"
	}
	return strings.Join(nodes, ", ")
}

func OrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func Keepalive(ms int) string {
	if ms <= 0 {
		return "—"
	}
	return fmt.Sprintf("%ds window", ms/1000)
}

func NavClass(active, key string) string {
	if active == key {
		return "sidebar-link active"
	}
	return "sidebar-link"
}

func IntText(n int) string {
	return fmt.Sprint(n)
}

func TotalUsers(stats *rpc.UserStats) string {
	if stats == nil {
		return "—"
	}
	return fmt.Sprint(stats.TotalUsers)
}

func ActiveUsers(stats *rpc.UserStats) string {
	if stats == nil {
		return "—"
	}
	return fmt.Sprint(stats.ActiveUsers)
}

func PremiumUsers(stats *rpc.UserStats) string {
	if stats == nil {
		return "—"
	}
	return fmt.Sprint(stats.PremiumUsers)
}

func VIPUsers(stats *rpc.UserStats) string {
	if stats == nil {
		return "—"
	}
	return fmt.Sprint(stats.VIPUsers)
}

func PaidUsers(stats *rpc.UserStats) string {
	if stats == nil {
		return "—"
	}
	return fmt.Sprint(stats.PaidUsers)
}

func ShardTotal(snap *rpc.Snapshot) int {
	if snap == nil {
		return 0
	}
	if snap.ShardCount > 0 {
		return snap.ShardCount
	}
	return len(snap.Shards)
}

func ConnectedShards(snap *rpc.Snapshot) int {
	if snap == nil {
		return 0
	}
	total := 0
	for _, shard := range snap.Shards {
		if shard.State == "connected" {
			total++
		}
	}
	return total
}

func DegradedShards(snap *rpc.Snapshot) int {
	total := ShardTotal(snap)
	connected := ConnectedShards(snap)
	if total < connected {
		return 0
	}
	return total - connected
}

func NodeCount(snap *rpc.Snapshot) int {
	if snap == nil {
		return 0
	}
	return len(snap.Nodes)
}

func HealthLabel(snap *rpc.Snapshot, errMsg string) string {
	if errMsg != "" || snap == nil {
		return "unreachable"
	}
	if DegradedShards(snap) == 0 {
		return "healthy"
	}
	return "degraded"
}
