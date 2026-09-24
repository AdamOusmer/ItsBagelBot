// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cache

import "strconv"

const (
	maxUint64Digits   = 20
	separatorColonLen = 1
)

func UserKey(prefix string, userID uint64) string {
	buf := make([]byte, 0, len(prefix)+maxUint64Digits)
	buf = append(buf, prefix...)
	buf = strconv.AppendUint(buf, userID, 10)
	return string(buf)
}

func PairKey(prefix string, id uint64, name string) string {
	buf := make([]byte, 0, len(prefix)+maxUint64Digits+separatorColonLen+len(name))
	buf = append(buf, prefix...)
	buf = strconv.AppendUint(buf, id, 10)
	buf = append(buf, ':')
	buf = append(buf, name...)
	return string(buf)
}
