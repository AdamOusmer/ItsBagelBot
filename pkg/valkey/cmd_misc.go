// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	valkey_go "github.com/valkey-io/valkey-go"
)

// SetIfAbsentGetExpireAtCmd writes value only if the key is absent, always
// returning the key's current value, and sets an absolute expiry in seconds.
func SetIfAbsentGetExpireAtCmd(client valkey_go.Client, key, value string, expireAtUnixSeconds string) valkey_go.Completed {
	return client.B().Arbitrary("SET").Keys(key).Args(value, "NX", "GET", "EXAT", expireAtUnixSeconds).Build()
}
