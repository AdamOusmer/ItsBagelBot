// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	valkey_go "github.com/valkey-io/valkey-go"
)

// HashFieldValue is one field and its value for a multi-field HSET.
type HashFieldValue struct {
	Field string
	Value string
}

// HGetCmd reads one hash field.
func HGetCmd(client valkey_go.Client, key, field string) valkey_go.Completed {
	return client.B().Hget().Key(key).Field(field).Build()
}

// HMGetCmd reads several hash fields in the given order.
func HMGetCmd(client valkey_go.Client, key string, fields ...string) valkey_go.Completed {
	return client.B().Hmget().Key(key).Field(fields...).Build()
}

// HSetCmd writes one or more hash fields in one command.
func HSetCmd(client valkey_go.Client, key string, pairs ...HashFieldValue) valkey_go.Completed {
	b := client.B().Hset().Key(key).FieldValue()
	for _, pair := range pairs {
		b = b.FieldValue(pair.Field, pair.Value)
	}
	return b.Build()
}

// HDelCmd removes one or more hash fields in one command.
func HDelCmd(client valkey_go.Client, key string, fields ...string) valkey_go.Completed {
	return client.B().Hdel().Key(key).Field(fields...).Build()
}

// HIncrByCmd increments one hash field by n.
func HIncrByCmd(client valkey_go.Client, key, field string, n int64) valkey_go.Completed {
	return client.B().Hincrby().Key(key).Field(field).Increment(n).Build()
}

// HKeysCmd lists a hash's field names.
func HKeysCmd(client valkey_go.Client, key string) valkey_go.Completed {
	return client.B().Hkeys().Key(key).Build()
}
