// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package live

import (
	"strconv"
	"time"
)

// Write only via pkg/valkey VersionedSlot; a plain SET or DEL breaks version ordering.
const KeyPrefix = "live:"

const InvalidateScope = "live"

func Key(id uint64) string { return KeyPrefix + strconv.FormatUint(id, 10) }

func KeyString(id string) string { return KeyPrefix + id }

const VerKeyPrefix = KeyPrefix + "ver:"

func VerKey(id uint64) string { return VerKeyPrefix + strconv.FormatUint(id, 10) }

func VerKeyString(id string) string { return VerKeyPrefix + id }

const VerTTL = 48 * time.Hour

func VersionNow() int64 { return time.Now().UnixMilli() }

func Value(version int64) string { return strconv.FormatInt(version, 10) }
