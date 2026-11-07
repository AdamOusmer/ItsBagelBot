// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	valkey_go "github.com/valkey-io/valkey-go"
)

// ExpireCmd sets a key's TTL unconditionally.
func ExpireCmd(client valkey_go.Client, key string, seconds int64) valkey_go.Completed {
	return client.B().Expire().Key(key).Seconds(seconds).Build()
}

// ExpireNXCmd sets a key's TTL only if it has none yet.
func ExpireNXCmd(client valkey_go.Client, key string, seconds int64) valkey_go.Completed {
	return client.B().Expire().Key(key).Seconds(seconds).Nx().Build()
}

// ExpireGTCmd raises a key's TTL only if that makes it longer.
func ExpireGTCmd(client valkey_go.Client, key string, seconds int64) valkey_go.Completed {
	return client.B().Expire().Key(key).Seconds(seconds).Gt().Build()
}

// ExpireAtCmd sets a key's TTL to an absolute Unix-seconds timestamp.
func ExpireAtCmd(client valkey_go.Client, key string, unixSeconds int64) valkey_go.Completed {
	return client.B().Expireat().Key(key).Timestamp(unixSeconds).Build()
}
