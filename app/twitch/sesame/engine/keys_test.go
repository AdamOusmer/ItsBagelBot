// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import "testing"

// TestKeyLiterals pins the exact bytes of the broadcaster-scoped Valkey keys.
// The builders were hand-rolled concatenations before they moved onto
// pkg/cache.UserKey/PairKey; a key that changes shape orphans every live entry
// written under the old one (balances go to zero, armed timers never fire), and
// nothing else in the suite would notice, because both sides of a round trip
// would agree on the new spelling. Hence literals here, not a re-derivation.
func TestKeyLiterals(t *testing.T) {
	const id uint64 = 12345
	got := map[string]string{
		"greet":           greetKey(id),
		"queue_open":      queueOpenKey(id),
		"queue_line":      queueLineKey(id),
		"loyalty_tick":    loyaltyTickKey(id),
		"recent":          recentChannelKey(channelID(id)),
		"songqueue_doc":   string(songQueueDocKey(id)),
		"emoteplay":       emoteplayKey(id),
		"personality":     personalityKey("fact", id),
		"counter_channel": counterRef{id, "hydrate"}.channelKey(),
		"counter_viewer":  counterRef{id, "hydrate"}.viewerKey(),
		"counter_scope":   counterRef{id, "hydrate"}.scopeKey(),
		"balance":         balanceKey(id, 678),
		"timer":           timerKey(id, "t1"),
		"campaign":        campaignKey(id, 255),
	}
	want := map[string]string{
		"greet":           "bagel:greeted:12345",
		"queue_open":      "queue:open:12345",
		"queue_line":      "queue:line:12345",
		"loyalty_tick":    "loyaltick:12345",
		"recent":          "am:recent:12345",
		"songqueue_doc":   "songqueue:doc:12345",
		"emoteplay":       "emoteplay:v1:12345",
		"personality":     "personality:fact:12345",
		"counter_channel": "loyal:cnt:c:12345:hydrate",
		"counter_viewer":  "loyal:cnt:v:12345:hydrate",
		"counter_scope":   "scope:12345:hydrate",
		"balance":         "loyal:bal:12345:678",
		"timer":           "timer:12345:t1",
		"campaign":        "am:tmpl:12345:ff",
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s key = %q, want %q", name, got[name], w)
		}
	}
}
