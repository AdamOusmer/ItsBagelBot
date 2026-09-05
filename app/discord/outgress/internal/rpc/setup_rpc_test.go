// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"slices"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"go.uber.org/zap"
)

// setupRPC builds the handler with no Discord client attached. Validation
// runs BEFORE the fill, so a refused request never reaches the client at
// all and an accepted one is recognisable by the "unavailable" it then
// fails with.
func setupRPC() *discordRPC {
	w := setup.New(setup.Config{Store: discordstore.NewMem(), Log: zap.NewNop()})
	return &discordRPC{w: w, log: zap.NewNop()}
}

// A real guild id: ValidateConfig refuses anything that cannot be a
// snowflake, this file included.
const rpcGuildID = "100000000000000001"

func TestSetupRefusesAPinnedRoleThatIsNotASnowflake(t *testing.T) {
	got := setupRPC().handleSetup(context.Background(), outgressrpc.DiscordSetupRequest{
		UserID: "42", GuildID: rpcGuildID,
		PinnedRoles: map[string]string{ddiscord.SlotMods: "<@&12345>"},
	})

	if got.Code != outgressrpc.CodeInvalid {
		t.Fatalf("code = %q, want %q", got.Code, outgressrpc.CodeInvalid)
	}
	if !slices.Equal(got.Fields, []string{"pinnedRoles"}) {
		t.Fatalf("fields = %v, want [pinnedRoles]", got.Fields)
	}
	if got.Error == "" {
		t.Fatal("a refusal must still carry prose for the release that predates code")
	}
}

// A slot that is not part of the template is refused rather than silently
// dropped: the dashboard sent something this build does not understand, and
// filling the guild anyway hides that from the streamer until they notice
// the role never got applied.
func TestSetupRefusesAnUnknownPinnedSlot(t *testing.T) {
	got := setupRPC().handleSetup(context.Background(), outgressrpc.DiscordSetupRequest{
		UserID: "42", GuildID: rpcGuildID,
		PinnedRoles: map[string]string{"janitor": "100000000000000002"},
	})

	if got.Code != outgressrpc.CodeInvalid {
		t.Fatalf("code = %q, want %q", got.Code, outgressrpc.CodeInvalid)
	}
}

// A guild id that cannot be a snowflake is refused before the bind, so a
// paste error never writes a binding nobody can unbind.
func TestSetupRefusesAGuildIDThatIsNotASnowflake(t *testing.T) {
	got := setupRPC().handleSetup(context.Background(), outgressrpc.DiscordSetupRequest{
		UserID: "42", GuildID: "my-server",
	})

	if got.Code != outgressrpc.CodeInvalid {
		t.Fatalf("code = %q, want %q", got.Code, outgressrpc.CodeInvalid)
	}
	if !slices.Equal(got.Fields, []string{"guildId"}) {
		t.Fatalf("fields = %v, want [guildId]", got.Fields)
	}
}

// Valid pins pass validation and reach the fill, which is what fails here
// (no client). The point is the absence of CodeInvalid: a well-formed
// request must not be refused by the validator.
func TestSetupAcceptsWellFormedPins(t *testing.T) {
	got := setupRPC().handleSetup(context.Background(), outgressrpc.DiscordSetupRequest{
		UserID: "42", GuildID: rpcGuildID,
		PinnedRoles: map[string]string{ddiscord.SlotMods: "100000000000000002"},
	})

	if got.Code == outgressrpc.CodeInvalid {
		t.Fatalf("a well-formed request was refused: %+v", got)
	}
	if got.Error == "" {
		t.Fatal("the fill should have failed with no discord client")
	}
}
