// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	valkey_go "github.com/valkey-io/valkey-go"
)

// GetCmd reads a plain string key.
func GetCmd(client valkey_go.Client, key string) valkey_go.Completed {
	return client.B().Get().Key(key).Build()
}

// SetCmd writes a plain string key with no expiry.
func SetCmd(client valkey_go.Client, key, value string) valkey_go.Completed {
	return client.B().Set().Key(key).Value(value).Build()
}

// SetExSecondsCmd writes a plain string key that expires after seconds.
func SetExSecondsCmd(client valkey_go.Client, key, value string, seconds int64) valkey_go.Completed {
	return client.B().Set().Key(key).Value(value).ExSeconds(seconds).Build()
}

// SetNXCmd writes a plain string key only if it is absent, with no expiry.
func SetNXCmd(client valkey_go.Client, key, value string) valkey_go.Completed {
	return client.B().Set().Key(key).Value(value).Nx().Build()
}

// DelCmd removes one or more keys.
func DelCmd(client valkey_go.Client, keys ...string) valkey_go.Completed {
	return client.B().Del().Key(keys...).Build()
}

// ExistsCmd counts how many of the given keys exist.
func ExistsCmd(client valkey_go.Client, keys ...string) valkey_go.Completed {
	return client.B().Exists().Key(keys...).Build()
}

// IncrByCmd increments an integer key by n.
func IncrByCmd(client valkey_go.Client, key string, n int64) valkey_go.Completed {
	return client.B().Incrby().Key(key).Increment(n).Build()
}
