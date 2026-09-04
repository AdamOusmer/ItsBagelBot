// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/discord/engine/module"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/discord/linkguard"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// fakeGuard is a Guarder that returns canned Verdicts by normalized link,
// falling back to a below-threshold allow for anything not stubbed. It
// never touches Valkey (or any network) at all, matching this task's "no
// network" style for the neighbouring linkguard package's own tests.
type fakeGuard struct {
	verdicts map[string]linkguard.Verdict
	seen     []linkguard.Sighting
}

func (f *fakeGuard) Observe(_ context.Context, s linkguard.Sighting) linkguard.Verdict {
	f.seen = append(f.seen, s)
	norm, invite := linkguard.NormalizeLink(s.Link)
	if v, ok := f.verdicts[norm]; ok {
		return v
	}
	return linkguard.Verdict{Allow: true, Reason: linkguard.ReasonBelowThreshold, NormalizedLink: norm, IsInvite: invite}
}

// trip returns a tripped, non-allow Verdict for link under reason.
func trip(link, reason string) linkguard.Verdict {
	norm, invite := linkguard.NormalizeLink(link)
	return linkguard.Verdict{Allow: false, Reason: reason, NormalizedLink: norm, IsInvite: invite, GuildTripped: true}
}

// fakeOwnInvite is an OwnInviteChecker that returns a canned answer (or a
// canned error) per raw link, and records every link it was asked about --
// this is what proves resolution only ever happens lazily: never for an
// Allowed link, never for a non-invite link, at most once per tripped
// invite link in one message. It never touches Valkey or the RPC bus, the
// same "no network" style as fakeGuard above.
type fakeOwnInvite struct {
	own map[string]bool // raw link -> IsOwnGuildInvite's answer
	err error           // returned instead of a canned answer when set

	calls []string // raw links this was asked about, in call order
}

func (f *fakeOwnInvite) IsOwnGuildInvite(_ context.Context, _ string, rawLink string) (bool, error) {
	f.calls = append(f.calls, rawLink)
	if f.err != nil {
		return false, f.err
	}
	return f.own[rawLink], nil
}

// messageEventInput is messageEventRaw's (id, guildID, channelID, authorID,
// content, bot, roles) tuple collapsed into one struct, matching this
// codebase's convention for bundling a call's varying parts (see
// linkguard.Sighting) (CodeScene: Excess Number of Function Arguments).
type messageEventInput struct {
	ID        string
	GuildID   string
	ChannelID string
	AuthorID  string
	Content   string
	Bot       bool
	Roles     []string
}

// messageEventRaw marshals the minimal MESSAGE_CREATE JSON shape
// decode.MessageEvent reads, roles included, matching what ingress relays
// verbatim from Discord's gateway.
func messageEventRaw(t *testing.T, in messageEventInput) []byte {
	t.Helper()
	raw, err := codec.Marshal(map[string]any{
		"id": in.ID, "guild_id": in.GuildID, "channel_id": in.ChannelID, "content": in.Content,
		"author": map[string]any{"id": in.AuthorID, "bot": in.Bot},
		"member": map[string]any{"roles": in.Roles},
	})
	if err != nil {
		t.Fatalf("marshal message event: %v", err)
	}
	return raw
}

func linkGuardContext(cfg ddiscord.Config, raw []byte) *module.Context {
	return &module.Context{
		Event:         ddiscord.Event{Type: "MESSAGE_CREATE", GuildID: cfg.GuildID, Raw: raw},
		Config:        cfg,
		BroadcasterID: "999",
		Log:           zap.NewNop(),
	}
}

func onGuardConfig() ddiscord.Config {
	return ddiscord.Config{GuildID: "g1", ModsRoleID: "modsrole", LinkGuardEnabled: "on"}
}

// runLinkGuard drives one MESSAGE_CREATE through LinkGuard's handler and
// collects whatever Commands it emits, using an OwnInviteChecker that is
// never consulted for a non-tripping run (returns false, nil if it is).
// Tests that care about invite resolution itself use runLinkGuardWithOwn
// directly so they can inspect fakeOwnInvite.calls afterward.
func runLinkGuard(t *testing.T, guard *fakeGuard, cfg ddiscord.Config, raw []byte) []ddiscord.Command {
	t.Helper()
	return runLinkGuardWithOwn(t, guard, &fakeOwnInvite{}, cfg, raw)
}

func runLinkGuardWithOwn(t *testing.T, guard *fakeGuard, own *fakeOwnInvite, cfg ddiscord.Config, raw []byte) []ddiscord.Command {
	t.Helper()
	mod := LinkGuard(guard, own, zap.NewNop())
	handler, ok := mod.Events["MESSAGE_CREATE"]
	if !ok {
		t.Fatal("LinkGuard did not register MESSAGE_CREATE")
	}
	var emitted []ddiscord.Command
	err := handler(context.Background(), linkGuardContext(cfg, raw), func(c ddiscord.Command) { emitted = append(emitted, c) })
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return emitted
}

func deleteCommands(cmds []ddiscord.Command) []ddiscord.Command {
	var out []ddiscord.Command
	for _, c := range cmds {
		if c.Type == ddiscord.TypeDeleteMessage {
			out = append(out, c)
		}
	}
	return out
}

func TestLinkGuardBelowThresholdUntouched(t *testing.T) {
	guard := &fakeGuard{verdicts: map[string]linkguard.Verdict{}}
	raw := messageEventRaw(t, messageEventInput{ID: "m1", GuildID: "g1", ChannelID: "c1", AuthorID: "u1", Content: "check out discord.gg/abc123"})

	cmds := runLinkGuard(t, guard, onGuardConfig(), raw)

	if got := deleteCommands(cmds); len(got) != 0 {
		t.Fatalf("delete commands = %d, want 0 (cmds %+v)", len(got), got)
	}
	if len(guard.seen) != 1 {
		t.Fatalf("guard.Observe called %d times, want 1", len(guard.seen))
	}
}

func TestLinkGuardThresholdTripDeletesWithReason(t *testing.T) {
	const link = "discord.gg/spamcode"
	norm, _ := linkguard.NormalizeLink(link)
	guard := &fakeGuard{verdicts: map[string]linkguard.Verdict{norm: trip(link, linkguard.ReasonChannelThreshold)}}
	raw := messageEventRaw(t, messageEventInput{ID: "m1", GuildID: "g1", ChannelID: "c3", AuthorID: "u1", Content: "join now " + link})

	cmds := runLinkGuard(t, guard, onGuardConfig(), raw)

	dels := deleteCommands(cmds)
	if len(dels) != 1 {
		t.Fatalf("delete commands = %d, want exactly 1 (cmds %+v)", len(dels), cmds)
	}
	d := dels[0]
	if d.ChannelID != "c3" || d.GuildID != "g1" {
		t.Errorf("delete targets guild=%q channel=%q, want g1/c3", d.GuildID, d.ChannelID)
	}
	if d.Reason == "" {
		t.Error("Reason is empty, want the tripped threshold recorded for the audit log")
	}
	var payload ddiscord.DeletePayload
	if err := codec.Unmarshal(d.Payload, &payload); err != nil {
		t.Fatalf("unmarshal delete payload: %v", err)
	}
	if payload.MessageID != "m1" {
		t.Errorf("MessageID = %q, want m1", payload.MessageID)
	}
}

func TestLinkGuardModeratorRepostExempt(t *testing.T) {
	const link = "discord.gg/spamcode"
	guard := &fakeGuard{verdicts: map[string]linkguard.Verdict{}}
	raw := messageEventRaw(t, messageEventInput{ID: "m1", GuildID: "g1", ChannelID: "c1", AuthorID: "u1", Content: link, Roles: []string{"modsrole"}})

	cmds := runLinkGuard(t, guard, onGuardConfig(), raw)

	if len(deleteCommands(cmds)) != 0 {
		t.Fatalf("delete commands present for a moderator repost, want none (cmds %+v)", cmds)
	}
	if len(guard.seen) != 1 || !guard.seen[0].Moderator {
		t.Fatalf("Sighting.Moderator = %v, want true", guard.seen[0].Moderator)
	}
}

func TestLinkGuardAllowListedExempt(t *testing.T) {
	const link = "discord.gg/partner"
	guard := &fakeGuard{verdicts: map[string]linkguard.Verdict{}}
	cfg := onGuardConfig()
	cfg.LinkAllowList = "discord.gg/partner"
	raw := messageEventRaw(t, messageEventInput{ID: "m1", GuildID: "g1", ChannelID: "c1", AuthorID: "u1", Content: link})

	cmds := runLinkGuard(t, guard, cfg, raw)

	if len(deleteCommands(cmds)) != 0 {
		t.Fatalf("delete commands present for an allow-listed link, want none (cmds %+v)", cmds)
	}
	if len(guard.seen) != 1 || !guard.seen[0].Allowed {
		t.Fatalf("Sighting.Allowed = %v, want true", guard.seen[0].Allowed)
	}
}

// TestLinkGuardOwnInviteTripIsNotDeleted is the regression test for the bug
// this RPC exists to fix: a guild pinning its OWN invite across enough
// channels to trip ChannelThreshold (e.g. #rules, #welcome,
// #announcements) must not have that message deleted. The Verdict still
// trips (guard.Observe was called with OwnGuildInvite always false -- see
// observeLinks' doc), but tripIsOwnInvite resolves it afterward and
// suppresses the action.
func TestLinkGuardOwnInviteTripIsNotDeleted(t *testing.T) {
	const link = "discord.gg/ourownserver"
	guard := &fakeGuard{verdicts: map[string]linkguard.Verdict{}}
	norm, _ := linkguard.NormalizeLink(link)
	guard.verdicts[norm] = trip(link, linkguard.ReasonChannelThreshold)
	own := &fakeOwnInvite{own: map[string]bool{link: true}}
	raw := messageEventRaw(t, messageEventInput{ID: "m1", GuildID: "g1", ChannelID: "c1", AuthorID: "u1", Content: link})

	cmds := runLinkGuardWithOwn(t, guard, own, onGuardConfig(), raw)

	if len(deleteCommands(cmds)) != 0 {
		t.Fatalf("delete commands present for the guild's own invite, want none (cmds %+v)", cmds)
	}
	if len(own.calls) != 1 || own.calls[0] != link {
		t.Fatalf("IsOwnGuildInvite calls = %v, want exactly [%q]", own.calls, link)
	}
	// The sighting itself is still counted -- see tripIsOwnInvite's doc for
	// why that is acceptable rather than a corrupted verdict.
	if len(guard.seen) != 1 {
		t.Fatalf("guard.Observe called %d times, want 1 (the trip is still counted)", len(guard.seen))
	}
}

// TestLinkGuardOtherGuildInviteStillDeleted proves the fix is narrow: an
// invite that resolves to some OTHER guild still gets deleted once
// tripped, exactly like before this RPC existed.
func TestLinkGuardOtherGuildInviteStillDeleted(t *testing.T) {
	const link = "discord.gg/someoneelses"
	guard := &fakeGuard{verdicts: map[string]linkguard.Verdict{}}
	norm, _ := linkguard.NormalizeLink(link)
	guard.verdicts[norm] = trip(link, linkguard.ReasonChannelThreshold)
	own := &fakeOwnInvite{own: map[string]bool{link: false}}
	raw := messageEventRaw(t, messageEventInput{ID: "m1", GuildID: "g1", ChannelID: "c1", AuthorID: "u1", Content: link})

	cmds := runLinkGuardWithOwn(t, guard, own, onGuardConfig(), raw)

	if len(deleteCommands(cmds)) != 1 {
		t.Fatalf("delete commands = %d, want exactly 1 for another guild's invite (cmds %+v)", len(deleteCommands(cmds)), cmds)
	}
	if len(own.calls) != 1 {
		t.Fatalf("IsOwnGuildInvite calls = %d, want 1", len(own.calls))
	}
}

// TestLinkGuardResolutionNotAttemptedForNonTrippingLink proves the lazy
// half of the cost guard: a link that never crosses a linkguard threshold
// (guard.Observe's default fake Verdict is Allow) must never trigger
// invite resolution at all -- resolving on every posted link, rather than
// only a tripped one, is exactly the REST-call amplification LinkGuard's
// own doc warns about.
func TestLinkGuardResolutionNotAttemptedForNonTrippingLink(t *testing.T) {
	guard := &fakeGuard{verdicts: map[string]linkguard.Verdict{}}
	own := &fakeOwnInvite{}
	raw := messageEventRaw(t, messageEventInput{ID: "m1", GuildID: "g1", ChannelID: "c1", AuthorID: "u1", Content: "check out discord.gg/abc123"})

	runLinkGuardWithOwn(t, guard, own, onGuardConfig(), raw)

	if len(own.calls) != 0 {
		t.Fatalf("IsOwnGuildInvite called %d times for a non-tripping link, want 0", len(own.calls))
	}
}

// TestLinkGuardOwnInviteRPCFailureSkipsAction proves the documented
// fail-safe direction: when resolution itself cannot be completed (the RPC
// or the Discord call behind it failed), the module treats the link as the
// guild's own -- i.e. it does NOT delete -- rather than proceeding with the
// trip. See tripIsOwnInvite's doc for why a false delete against a real
// community was judged worse than a missed spam message during an outage.
func TestLinkGuardOwnInviteRPCFailureSkipsAction(t *testing.T) {
	const link = "discord.gg/unresolvable"
	guard := &fakeGuard{verdicts: map[string]linkguard.Verdict{}}
	norm, _ := linkguard.NormalizeLink(link)
	guard.verdicts[norm] = trip(link, linkguard.ReasonChannelThreshold)
	own := &fakeOwnInvite{err: errors.New("outgress rpc timeout")}
	raw := messageEventRaw(t, messageEventInput{ID: "m1", GuildID: "g1", ChannelID: "c1", AuthorID: "u1", Content: link})

	cmds := runLinkGuardWithOwn(t, guard, own, onGuardConfig(), raw)

	if len(deleteCommands(cmds)) != 0 {
		t.Fatalf("delete commands present after an unresolvable invite check, want none -- must fail safe (cmds %+v)", cmds)
	}
}

func TestLinkGuardBotAuthorIgnored(t *testing.T) {
	guard := &fakeGuard{verdicts: map[string]linkguard.Verdict{}}
	raw := messageEventRaw(t, messageEventInput{ID: "m1", GuildID: "g1", ChannelID: "c1", AuthorID: "bot1", Content: "discord.gg/spamcode", Bot: true})

	cmds := runLinkGuard(t, guard, onGuardConfig(), raw)

	if len(cmds) != 0 {
		t.Fatalf("commands emitted for a bot author, want none (cmds %+v)", cmds)
	}
	if len(guard.seen) != 0 {
		t.Fatalf("guard.Observe called for a bot author, want 0 calls")
	}
}

func TestLinkGuardValkeyErrorAllows(t *testing.T) {
	const link = "discord.gg/spamcode"
	norm, _ := linkguard.NormalizeLink(link)
	// A Valkey error surfaces from linkguard.Observe as an Allow verdict
	// (see linkguard.ReasonValkeyError's doc) -- this fake reproduces that
	// exact shape, since the module itself has no error path to special-
	// case: Allow: true is Allow: true regardless of why.
	guard := &fakeGuard{verdicts: map[string]linkguard.Verdict{
		norm: {Allow: true, Reason: linkguard.ReasonValkeyError, NormalizedLink: norm},
	}}
	raw := messageEventRaw(t, messageEventInput{ID: "m1", GuildID: "g1", ChannelID: "c1", AuthorID: "u1", Content: link})

	cmds := runLinkGuard(t, guard, onGuardConfig(), raw)

	if len(deleteCommands(cmds)) != 0 {
		t.Fatalf("delete commands present after a Valkey error, want none -- must fail open (cmds %+v)", cmds)
	}
}

func TestLinkGuardThreeLinksAtMostOneDelete(t *testing.T) {
	links := []string{"discord.gg/one", "discord.gg/two", "discord.gg/three"}
	verdicts := map[string]linkguard.Verdict{}
	for _, l := range links {
		norm, _ := linkguard.NormalizeLink(l)
		verdicts[norm] = trip(l, linkguard.ReasonChannelThreshold)
	}
	guard := &fakeGuard{verdicts: verdicts}
	raw := messageEventRaw(t, messageEventInput{ID: "m1", GuildID: "g1", ChannelID: "c1", AuthorID: "u1", Content: links[0] + " " + links[1] + " " + links[2]})

	cmds := runLinkGuard(t, guard, onGuardConfig(), raw)

	if got := deleteCommands(cmds); len(got) != 1 {
		t.Fatalf("delete commands = %d, want exactly 1 for one message with three tripped links (cmds %+v)", len(got), cmds)
	}
	if len(guard.seen) != 3 {
		t.Fatalf("guard.Observe called %d times, want 3 (every link still recorded)", len(guard.seen))
	}
}
