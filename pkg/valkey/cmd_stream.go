// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	valkey_go "github.com/valkey-io/valkey-go"
)

// XGroupCreateCmd creates a consumer group at the stream's start, creating the
// stream itself if it does not yet exist.
func XGroupCreateCmd(client valkey_go.Client, stream, group string) valkey_go.Completed {
	return client.B().XgroupCreate().Key(stream).Group(group).Id("0").Mkstream().Build()
}

// XRangeCmd reads up to count entries from the start of a stream.
func XRangeCmd(client valkey_go.Client, stream string, count int64) valkey_go.Completed {
	return client.B().Xrange().Key(stream).Start("-").End("+").Count(count).Build()
}

// GroupRead names a consumer group read: up to Count new deliveries, blocking up to BlockMS.
type GroupRead struct {
	Stream   string
	Group    string
	Consumer string
	Count    int64
	BlockMS  int64
}

// XReadGroupCmd blocks up to BlockMS milliseconds for up to Count new deliveries.
func XReadGroupCmd(client valkey_go.Client, read GroupRead) valkey_go.Completed {
	return client.B().Xreadgroup().Group(read.Group, read.Consumer).Count(read.Count).Block(read.BlockMS).Streams().Key(read.Stream).Id(">").Build()
}

// GroupReclaim names a pending-entry reclaim: up to Count deliveries idle at least MinIdleMS, starting at Cursor.
type GroupReclaim struct {
	Stream    string
	Group     string
	Consumer  string
	MinIdleMS string
	Cursor    string
	Count     int64
}

// XAutoClaimCmd reclaims up to Count deliveries idle at least MinIdleMS, starting at Cursor.
func XAutoClaimCmd(client valkey_go.Client, reclaim GroupReclaim) valkey_go.Completed {
	return client.B().Xautoclaim().Key(reclaim.Stream).Group(reclaim.Group).Consumer(reclaim.Consumer).MinIdleTime(reclaim.MinIdleMS).Start(reclaim.Cursor).Count(reclaim.Count).Build()
}
