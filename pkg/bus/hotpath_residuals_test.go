// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"

	"github.com/nats-io/nats.go"
)

// identitySlot asserts the pooled envelope's identity invariant — exactly one
// element, capacity one so no pool cycle ever reallocates it, read back through
// canonical lookup — and returns the current value.
func identitySlot(t *testing.T, h nats.Header) string {
	t.Helper()
	slot, ok := h[messageIDHeader]
	if !ok || len(slot) != 1 || cap(slot) != 1 {
		t.Fatalf("identity slot not an envelope-owned one-element slice: %+v cap %d", slot, cap(slot))
	}
	return slot[0]
}

func TestResetWireHeaderKeepsIdentitySlot(t *testing.T) {
	h := resetWireHeader(nil)
	if got := identitySlot(t, h); got != "" {
		t.Fatalf("fresh slot carries %q", got)
	}

	h[messageIDHeader][0] = "first"
	h.Set("X-Trace-Id", "abc")

	h = resetWireHeader(h)
	if len(h) != 1 {
		t.Fatalf("reset left %d keys, want only the identity slot", len(h))
	}
	if got := identitySlot(t, h); got != "" {
		t.Fatalf("slot not cleared for reuse: %q", got)
	}

	// Overwrite cycles stand in for publishes: the slot must take each new id
	// without growing, which is what makes the reuse allocation-free.
	for _, id := range []string{"a", "b", "c"} {
		h[messageIDHeader][0] = id
		if got := identitySlot(t, h); got != id {
			t.Fatalf("cycle %q reads back %q", id, got)
		}
	}
}

func TestResetWireHeaderTruncatesForeignIdentitySlice(t *testing.T) {
	h := nats.Header{
		messageIDHeader: {"x", "y"},
		"Other":         {"v"},
	}
	h = resetWireHeader(h)
	slot := identitySlot(t, h)
	if _, ok := h["Other"]; ok {
		t.Fatal("foreign key survived reset")
	}
}

func TestFleetMetadataIdentityOnlyReturnsNil(t *testing.T) {
	identityOnly := nats.Header{
		MessageIDHeader: {"id"},
		nats.MsgIdHdr:   {"dedupe"},
	}
	metadata, err := fleetMetadata(identityOnly)
	if err != nil {
		t.Fatalf("identity-only headers rejected: %v", err)
	}
	if metadata != nil {
		t.Fatalf("identity-only headers allocated %v", metadata)
	}
	if got := metadata.Get("anything"); got != "" {
		t.Fatalf("Get on nil metadata returned %q", got)
	}

	withTrace := nats.Header{
		MessageIDHeader: {"id"},
		"Traceparent":   {"00-trace-span"},
	}
	metadata, err = fleetMetadata(withTrace)
	if err != nil {
		t.Fatalf("mixed headers rejected: %v", err)
	}
	if len(metadata) != 1 || metadata["Traceparent"] != "00-trace-span" {
		t.Fatalf("mixed headers lost data: %v", metadata)
	}

	if _, err := fleetMetadata(nats.Header{"X-Dup": {"a", "b"}}); err == nil {
		t.Fatal("multi-value header accepted")
	}
}

func TestPublishInflightCohortsBounds(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"unset picks the historical default", "", maxInflightCohorts},
		{"explicit default accepted", "4", 4},
		{"raised within bound", "16", 16},
		{"ceiling held", "64", 64},
		{"above ceiling clamped", "65", 64},
		{"absurd value clamped", "100000", 64},
		{"floor keeps progress", "1", 1},
		{"zero clamped", "0", 1},
		{"negative clamped", "-8", 1},
		{"garbage falls back", "many", maxInflightCohorts},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NATS_PUBLISH_INFLIGHT_COHORTS", tc.raw)
			if got := publishInflightCohorts(); got != tc.want {
				t.Fatalf("NATS_PUBLISH_INFLIGHT_COHORTS=%q -> %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}
