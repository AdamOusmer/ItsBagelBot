// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"

	"go.uber.org/zap"
)

// The dashboard reads these fields off the wire, so the test pins the JSON
// rather than the Go struct. Each wire entry maps a fragment to whether the
// reply must carry it: a duplicate title must never trigger the reconnect CTA.
func TestChannelPointsFailWireShape(t *testing.T) {
	cp := &channelPoints{log: zap.NewNop()}
	cases := map[string]struct {
		err  error
		wire map[string]bool
	}{
		"duplicate title": {fmt.Errorf("create: %w", twitch.ErrDuplicateReward), map[string]bool{`"error":"`: true, `"code":"conflict"`: true, "missing_scope": false}},
		"missing scope":   {twitch.ErrMissingScope, map[string]bool{`"error":"`: true, `"code":"forbidden"`: true, `"missing_scope":true`: true}},
		"other":           {&twitch.StatusError{Status: 500, Op: "POST", Body: "boom"}, map[string]bool{`"error":"`: true, `"code":"internal"`: true, "missing_scope": false}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			requireWire(t, cp.fail("channelpoints create", "1", tc.err), tc.wire)
		})
	}
}

func requireWire(t *testing.T, reply manage.RewardReply, wire map[string]bool) {
	t.Helper()
	raw, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	for fragment, want := range wire {
		if strings.Contains(string(raw), fragment) != want {
			t.Errorf("reply %s: carries %s = %v, want %v", raw, fragment, !want, want)
		}
	}
}
