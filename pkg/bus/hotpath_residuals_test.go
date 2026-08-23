// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"

	"github.com/nats-io/nats.go"
)

func TestResetWireHeaderKeepsIdentitySlot(t *testing.T) {
	h := resetWireHeader(nil)
	slot, ok := h[messageIDHeader]
	if !ok || len(slot) != 1 || cap(slot) != 1 {
		t.Fatalf("fresh envelope missing one-element identity slot: %+v", slot)
	}

	h[messageIDHeader][0] = "first"
	h.Set("X-Trace-Id", "abc")

	h = resetWireHeader(h)
	if len(h) != 1 {
		t.Fatalf("reset left %d keys, want only the identity slot", len(h))
	}
	slot = h[messageIDHeader]
	if len(slot) != 1 || slot[0] != "" {
		t.Fatalf("identity slot not cleared for reuse: %q", slot)
	}
	if cap(slot) != 1 {
		t.Fatalf("identity slot reallocated across pool cycles: cap %d", cap(slot))
	}

	for i, id := range []string{"a", "b", "c"} {
		wire := &nats.Msg{Header: h}
		wire.Header[messageIDHeader][0] = id
		if got := wire.Header.Get(messageIDHeader); got != id {
			t.Fatalf("cycle %d: header reads %q, want %q", i, got, id)
		}
	}
	if cap(h[messageIDHeader]) != 1 {
		t.Fatalf("identity slot grew across cycles: cap %d", cap(h[messageIDHeader]))
	}
}

func TestResetWireHeaderTruncatesForeignIdentitySlice(t *testing.T) {
	h := nats.Header{
		messageIDHeader: {"x", "y"},
		"Other":         {"v"},
	}
	h = resetWireHeader(h)
	slot := h[messageIDHeader]
	if len(slot) != 1 || slot[0] != "" {
		t.Fatalf("identity slot not truncated to one cleared element: %q", slot)
	}
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
