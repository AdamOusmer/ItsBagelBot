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

// XReadGroupCmd blocks up to blockMS milliseconds for up to count new deliveries.
func XReadGroupCmd(client valkey_go.Client, stream, group, consumer string, count, blockMS int64) valkey_go.Completed {
	return client.B().Xreadgroup().Group(group, consumer).Count(count).Block(blockMS).Streams().Key(stream).Id(">").Build()
}

// XAutoClaimCmd reclaims up to count deliveries idle at least minIdleMS, starting at cursor.
func XAutoClaimCmd(client valkey_go.Client, stream, group, consumer, minIdleMS, cursor string, count int64) valkey_go.Completed {
	return client.B().Xautoclaim().Key(stream).Group(group).Consumer(consumer).MinIdleTime(minIdleMS).Start(cursor).Count(count).Build()
}
