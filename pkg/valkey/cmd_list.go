// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	valkey_go "github.com/valkey-io/valkey-go"
)

// LpushCmd prepends one element to a list.
func LpushCmd(client valkey_go.Client, key, element string) valkey_go.Completed {
	return client.B().Lpush().Key(key).Element(element).Build()
}

// LtrimCmd keeps only the elements between start and stop.
func LtrimCmd(client valkey_go.Client, key string, start, stop int64) valkey_go.Completed {
	return client.B().Ltrim().Key(key).Start(start).Stop(stop).Build()
}

// LrangeCmd reads the elements between start and stop.
func LrangeCmd(client valkey_go.Client, key string, start, stop int64) valkey_go.Completed {
	return client.B().Lrange().Key(key).Start(start).Stop(stop).Build()
}
