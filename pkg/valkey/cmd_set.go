// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	valkey_go "github.com/valkey-io/valkey-go"
)

// SaddCmd adds members to a set.
func SaddCmd(client valkey_go.Client, key string, members ...string) valkey_go.Completed {
	return client.B().Sadd().Key(key).Member(members...).Build()
}

// SremCmd removes members from a set.
func SremCmd(client valkey_go.Client, key string, members ...string) valkey_go.Completed {
	return client.B().Srem().Key(key).Member(members...).Build()
}

// ScardCmd counts a set's members.
func ScardCmd(client valkey_go.Client, key string) valkey_go.Completed {
	return client.B().Scard().Key(key).Build()
}
