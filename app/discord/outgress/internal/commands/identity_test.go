// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/kv"
	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

type fakeReauth struct {
	marked  []string
	cleared []string
}

func (f *fakeReauth) MarkNeedsReauth(_ context.Context, g kv.GuildID) error {
	f.marked = append(f.marked, string(g))
	return nil
}

func (f *fakeReauth) ClearNeedsReauth(_ context.Context, g kv.GuildID) error {
	f.cleared = append(f.cleared, string(g))
	return nil
}

func premiumIdentityCommand(t *testing.T) ddiscord.Command {
	t.Helper()
	return ddiscord.Command{
		Type: ddiscord.TypeSetGuildIdentity, GuildID: "g1",
		Payload: marshalPayload(t, ddiscord.IdentityPayload{Identity: ddiscord.GuildIdentity{Premium: true}}),
	}
}

// A premium apply must set BOTH halves in one call: the nickname needs
// CHANGE_NICKNAME, the avatar needs no permission, and sending them
// separately would leave a guild half-renamed whenever one of the two fails.
func TestSetGuildIdentityPremiumSendsNickAndAvatar(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}
	dispatchOK(t, h, ddiscord.Command{
		Type: ddiscord.TypeSetGuildIdentity, GuildID: "g1",
		Payload: marshalPayload(t, ddiscord.IdentityPayload{Identity: ddiscord.GuildIdentity{Premium: true}}),
	})
	if len(rest.identities) != 1 {
		t.Fatalf("calls = %d, want 1", len(rest.identities))
	}
	got := rest.identities[0]
	if got.Nick == nil || *got.Nick != ddiscord.PremiumNick {
		t.Fatalf("nick = %v, want %q", got.Nick, ddiscord.PremiumNick)
	}
	if got.AvatarDataURI == nil || !strings.HasPrefix(*got.AvatarDataURI, "data:image/png;base64,") {
		t.Fatal("premium apply did not carry a png data URI")
	}
}

// A downgrade must send explicit nulls. Omitting the fields means "leave
// unchanged" to Discord, which would strand the premium nickname on a guild
// whose streamer stopped paying.
func TestSetGuildIdentityDefaultClearsBothOverrides(t *testing.T) {
	rest := &fakeRest{}
	h := &Handlers{Rest: rest}
	dispatchOK(t, h, ddiscord.Command{
		Type: ddiscord.TypeSetGuildIdentity, GuildID: "g1",
		Payload: marshalPayload(t, ddiscord.IdentityPayload{Identity: ddiscord.GuildIdentity{Premium: false}}),
	})
	got := rest.identities[0]
	if got.Nick != nil || got.AvatarDataURI != nil {
		t.Fatalf("downgrade did not clear both overrides: nick=%v avatar set=%v", got.Nick, got.AvatarDataURI != nil)
	}
}

// A guild installed before CHANGE_NICKNAME refuses the whole call, avatar
// included. Retrying without the nick still lands the premium avatar, and
// the refusal is recorded so the dashboard can ask for a re-authorization.
func TestSetGuildIdentityForbiddenFallsBackToAvatarOnly(t *testing.T) {
	rest := &fakeRest{identityErrs: []error{discapi.ErrForbidden}}
	reauth := &fakeReauth{}
	h := &Handlers{Rest: rest, Reauth: reauth}
	dispatchOK(t, h, premiumIdentityCommand(t))

	if len(rest.identities) != 2 {
		t.Fatalf("calls = %d, want 2 (nick+avatar, then avatar only)", len(rest.identities))
	}
	retry := rest.identities[1]
	if retry.Nick != nil {
		t.Fatal("retry still carried a nick")
	}
	if retry.AvatarDataURI == nil {
		t.Fatal("retry dropped the avatar too, so the guild gets nothing")
	}
	requireOneCall(t, reauth.marked, func(g string) bool { return g == "g1" }, "marked")
}

// A permission error must not nack: it will refuse identically forever, and
// redelivering it just burns the shared per-token budget.
func TestSetGuildIdentityForbiddenDoesNotRetryForever(t *testing.T) {
	rest := &fakeRest{identityErrs: []error{discapi.ErrForbidden}}
	h := &Handlers{Rest: rest, Reauth: &fakeReauth{}}
	if err := h.Dispatch(context.Background(), premiumIdentityCommand(t)); err != nil {
		t.Fatalf("a frozen permission was surfaced as a retryable error: %v", err)
	}
}

// A success is the only proof the permission arrived, so it clears the flag.
func TestSetGuildIdentitySuccessClearsReauth(t *testing.T) {
	reauth := &fakeReauth{}
	h := &Handlers{Rest: &fakeRest{}, Reauth: reauth}
	dispatchOK(t, h, premiumIdentityCommand(t))
	if len(reauth.cleared) != 1 {
		t.Fatalf("cleared = %v, want one entry", reauth.cleared)
	}
}

// A non-permission failure must still nack, or a transient Discord blip
// would silently drop the identity change.
func TestSetGuildIdentityOtherErrorStillFails(t *testing.T) {
	rest := &fakeRest{identityErrs: []error{errors.New("boom")}}
	h := &Handlers{Rest: rest, Reauth: &fakeReauth{}}
	if err := h.Dispatch(context.Background(), premiumIdentityCommand(t)); err == nil {
		t.Fatal("transient error was swallowed")
	}
}
