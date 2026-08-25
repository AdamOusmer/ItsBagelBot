// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package validate holds the trust-boundary checks applied where outside data
// crosses into infrastructure that treats a value as safe to build keys from.
package validate

// Twitch user ids are bare decimal digits (Twitch mints them as int64s and
// never zero-pads), so digits-only is the complete shape check, not an
// approximation. The length bound is the widest int64 (19 digits); anything
// longer was never minted by Twitch.
const maxBroadcasterIDLen = 19

// BroadcasterID reports whether id is a well-formed numeric Twitch user id.
//
// WHY THIS EXISTS: broadcaster ids flow unchecked from producers and RPC
// callers into dynamic Valkey keys — ratelimit:chat:<id>, ratelimit:helix:*:<id>,
// outgress:channel:<id>, ratelimit:lookup:<id> — and into the channel registry
// index set. A caller able to inject arbitrary strings there can pollute or
// collide the key space (one crafted id makes every later message sharing it
// pay one attacker-chosen bucket) and permanently litter the registry index,
// because nothing ever expires those keys. Rejecting non-numeric ids at every
// decode boundary (lane Process, management RPCs) closes the injection point;
// the empty id stays legal here because several system paths legitimately omit
// it and are handled downstream.
//
// Discord snowflakes are also pure digits, so this check is safe to apply to
// any platform's id-shaped field; YouTube channel ids ("UC…") are NOT and must
// never route through it.
func BroadcasterID(id string) bool {
	if id == "" || len(id) > maxBroadcasterIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}
