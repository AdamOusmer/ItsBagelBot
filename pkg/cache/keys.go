package cache

import "strconv"

// UserKey builds "<prefix><userID>" with a single allocation. Hot paths key
// almost everything by Twitch user ID, so this avoids fmt and its reflection.
func UserKey(prefix string, userID uint64) string {

	buf := make([]byte, 0, len(prefix)+20) // 20 digits fit any uint64

	buf = append(buf, prefix...)
	buf = strconv.AppendUint(buf, userID, 10)

	return string(buf)
}
