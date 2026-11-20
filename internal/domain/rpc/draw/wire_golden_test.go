// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package drawrpc

import (
	"os"
	"testing"

	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

func TestRaffleStatusWireGolden(t *testing.T) {
	wire := struct {
		Subject string              `json:"subject"`
		Request RaffleStatusRequest `json:"request"`
		Reply   RaffleStatus        `json:"reply"`
		Empty   RaffleStatus        `json:"empty"`
		Refused RaffleStatus        `json:"refused"`
	}{
		Subject: RaffleStatusSubject,
		Request: RaffleStatusRequest{UserID: "7", Source: "dashboard"},
		Reply:   RaffleStatus{AccrualHealth: "healthy", Unpaid: []string{"unpaid"}},
		Empty:   RaffleStatus{AccrualHealth: "offline"},
		Refused: RaffleStatus{Refusal: rpc.Refused(rpc.CodeInvalid, "invalid user_id or source")},
	}
	actual, err := codec.MarshalIndent(wire, "", "  ")
	require.NoError(t, err)
	expected, err := os.ReadFile("testdata/wire.json")
	require.NoError(t, err)
	require.Equal(t, string(expected), string(actual)+"\n")
}

func TestGiveawayWireGolden(t *testing.T) {
	var delivered bool
	var count int64
	empty := []GiveawayRound{}
	for _, tc := range []struct {
		name string
		wire any
		json string
	}{
		{"delivery reset", GiveawayRequest{UserID: "7", Source: "dashboard", ActorID: "8", RoundID: "round", Slot: 2, Delivered: &delivered},
			`{"user_id":"7","source":"dashboard","actor_id":"8","round_id":"round","slot":2,"delivered":false}`},
		{"eligible count zero", GiveawayReply{Count: &count}, `{"count":0}`},
		{"empty live", GiveawayReply{Rounds: &empty}, `{"rounds":[]}`},
		{"live round", GiveawayRound{ID: "round", Kind: "giveaway", Phase: "settling", Prize: "Game",
			Winners: 2, OpenedMS: 1, ClosesMS: 2, Entries: 9, Refused: 3,
			Requirements: []string{"Followers only"}, Slots: []GiveawaySlot{{N: 1, Generation: 3, State: "verifying", CanSkipRule: true}}},
			`{"id":"round","kind":"giveaway","phase":"settling","prize":"Game","winners":2,"opened_ms":1,"closes_ms":2,"entries":9,"refused":3,"requirements":["Followers only"],"slots":[{"n":1,"generation":3,"state":"verifying","can_skip_rule":true}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := codec.Marshal(tc.wire)
			require.NoError(t, err)
			require.JSONEq(t, tc.json, string(wire))
		})
	}
}
