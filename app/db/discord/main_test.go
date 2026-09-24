// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import "testing"

func TestRPCEndpointPrefersTheRPCPlane(t *testing.T) {
	cases := []struct {
		name   string
		busURL string
		rpcURL string
		want   string
	}{
		{name: "defaults to the local endpoint", want: defaultNATSURL},
		{name: "falls back to NATS_URL", busURL: "nats://bus:4222", want: "nats://bus:4222"},
		{
			name:   "prefers NATS_RPC_URL",
			busURL: "nats://bus:4222",
			rpcURL: "tls://nats-leaf.messaging:4222",
			want:   "tls://nats-leaf.messaging:4222",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NATS_URL", tc.busURL)
			t.Setenv("NATS_RPC_URL", tc.rpcURL)
			if got := rpcEndpoint(); got != tc.want {
				t.Fatalf("rpcEndpoint() = %q, want %q", got, tc.want)
			}
		})
	}
}
