// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cache

import "strconv"

const (
	maxUint64Digits   = 20
	separatorColonLen = 1
)

// UserKey builds "<prefix><userID>" with a single allocation. Hot paths key
// almost everything by Twitch user ID, so this avoids fmt and its reflection.
func UserKey(prefix string, userID uint64) string {
	buf := make([]byte, 0, len(prefix)+maxUint64Digits)
	buf = append(buf, prefix...)
	buf = strconv.AppendUint(buf, userID, 10)
	return string(buf)
}

// PairKey builds "<prefix><id>:<name>" with a single allocation, for cache
// entries keyed by a user id plus a sub-key (e.g. a command name). Avoids fmt
// and its reflection on the hot path.
func PairKey(prefix string, id uint64, name string) string {
	buf := make([]byte, 0, len(prefix)+maxUint64Digits+separatorColonLen+len(name))
	buf = append(buf, prefix...)
	buf = strconv.AppendUint(buf, id, 10)
	buf = append(buf, ':')
	buf = append(buf, name...)
	return string(buf)
}
