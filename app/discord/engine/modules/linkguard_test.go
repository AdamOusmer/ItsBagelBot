// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"encoding/json"
	"testing"

	"ItsBagelBot/app/discord/engine/module"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/discord/linkguard"

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

// messageEventRaw marshals the minimal MESSAGE_CREATE JSON shape
// decode.MessageEvent reads, roles included, matching what ingress relays
// verbatim from Discord's gateway.
func messageEventRaw(t *testing.T, id, guildID, channelID, authorID, content string, bot bool, roles []string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"id": id, "guild_id": guildID, "channel_id": channelID, "content": content,
		"author": map[string]any{"id": authorID, "bot": bot},
		"member": map[string]any{"roles": roles},
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

// run drives one MESSAGE_CREATE through LinkGuard's handler and collects
// whatever Commands it emits.
func runLinkGuard(t *testing.T, guard *fakeGuard, cfg ddiscord.Config, raw []byte) []ddiscord.Command {
	t.Helper()
	mod := LinkGuard(guard, zap.NewNop())
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
	raw := messageEventRaw(t, "m1", "g1", "c1", "u1", "check out discord.gg/abc123", false, nil)

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
	raw := messageEventRaw(t, "m1", "g1", "c3", "u1", "join now "+link, false, nil)

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
	if err := json.Unmarshal(d.Payload, &payload); err != nil {
		t.Fatalf("unmarshal delete payload: %v", err)
	}
	if payload.MessageID != "m1" {
		t.Errorf("MessageID = %q, want m1", payload.MessageID)
	}
}

func TestLinkGuardModeratorRepostExempt(t *testing.T) {
	const link = "discord.gg/spamcode"
	guard := &fakeGuard{verdicts: map[string]linkguard.Verdict{}}
	raw := messageEventRaw(t, "m1", "g1", "c1", "u1", link, false, []string{"modsrole"})

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
	raw := messageEventRaw(t, "m1", "g1", "c1", "u1", link, false, nil)

	cmds := runLinkGuard(t, guard, cfg, raw)

	if len(deleteCommands(cmds)) != 0 {
		t.Fatalf("delete commands present for an allow-listed link, want none (cmds %+v)", cmds)
	}
	if len(guard.seen) != 1 || !guard.seen[0].Allowed {
		t.Fatalf("Sighting.Allowed = %v, want true", guard.seen[0].Allowed)
	}
}

// TestLinkGuardOwnGuildInviteAlwaysFalse pins a known limitation rather
// than asserting untested behavior: this module cannot resolve an
// arbitrary invite code to the guild it targets (see observeLinks'
// OwnGuildInvite comment for why -- no Discord REST client in engine, and
// no invite-code cache anywhere in this codebase today), so
// Sighting.OwnGuildInvite is always false. linkguard's own
// TestObserveOwnGuildInviteExempt already proves the exemption fires
// correctly once that field IS true; this test guards against this module
// silently starting to fabricate a true value it cannot actually verify.
// A guild that needs this exemption in practice gets the same Allow
// outcome via LinkAllowList today (see TestLinkGuardAllowListedExempt).
func TestLinkGuardOwnGuildInviteAlwaysFalse(t *testing.T) {
	guard := &fakeGuard{verdicts: map[string]linkguard.Verdict{}}
	raw := messageEventRaw(t, "m1", "g1", "c1", "u1", "discord.gg/ourownserver", false, nil)

	runLinkGuard(t, guard, onGuardConfig(), raw)

	if len(guard.seen) != 1 {
		t.Fatalf("guard.Observe called %d times, want 1", len(guard.seen))
	}
	if guard.seen[0].OwnGuildInvite {
		t.Fatal("Sighting.OwnGuildInvite = true, want false (see the comment on this test)")
	}
}

func TestLinkGuardBotAuthorIgnored(t *testing.T) {
	guard := &fakeGuard{verdicts: map[string]linkguard.Verdict{}}
	raw := messageEventRaw(t, "m1", "g1", "c1", "bot1", "discord.gg/spamcode", true, nil)

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
	raw := messageEventRaw(t, "m1", "g1", "c1", "u1", link, false, nil)

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
	raw := messageEventRaw(t, "m1", "g1", "c1", "u1",
		links[0]+" "+links[1]+" "+links[2], false, nil)

	cmds := runLinkGuard(t, guard, onGuardConfig(), raw)

	if got := deleteCommands(cmds); len(got) != 1 {
		t.Fatalf("delete commands = %d, want exactly 1 for one message with three tripped links (cmds %+v)", len(got), cmds)
	}
	if len(guard.seen) != 3 {
		t.Fatalf("guard.Observe called %d times, want 3 (every link still recorded)", len(guard.seen))
	}
}
