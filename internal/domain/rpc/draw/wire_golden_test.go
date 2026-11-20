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
