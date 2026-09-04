// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/discord/engine/module"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// fakeApplied is an in-memory AppliedStore. recordErr lets a test prove the
// module still emits when only the memory of the apply fails.
type fakeApplied struct {
	seen      map[string]string
	records   int
	recordErr error
}

func newFakeApplied() *fakeApplied { return &fakeApplied{seen: map[string]string{}} }

func (f *fakeApplied) Applied(_ context.Context, guildID string) (string, bool) {
	v, ok := f.seen[guildID]
	return v, ok
}

func (f *fakeApplied) Record(_ context.Context, guildID, fingerprint string) error {
	f.records++
	if f.recordErr != nil {
		return f.recordErr
	}
	f.seen[guildID] = fingerprint
	return nil
}

func identityFor(t *testing.T, applied *fakeApplied, status string) (*Identity, *[]ddiscord.Command) {
	t.Helper()
	var emitted []ddiscord.Command
	i := &Identity{
		Resolve: func(context.Context, uint64) (ddiscord.Config, bool) {
			return ddiscord.Config{GuildID: "g1"}, true
		},
		Status: func(context.Context, uint64) (string, bool) {
			if status == "" {
				return "", false
			}
			return status, true
		},
		Applied: applied,
		Publish: func(_ context.Context, c ddiscord.Command) error {
			emitted = append(emitted, c)
			return nil
		},
		Log: zap.NewNop(),
	}
	return i, &emitted
}

func runOnGuild(t *testing.T, i *Identity) []ddiscord.Command {
	t.Helper()
	mod := IdentityModule(i)
	handler, ok := mod.Events[ddiscord.SubjectEventGuild]
	if !ok {
		t.Fatal("IdentityModule did not register the guild event")
	}
	var emitted []ddiscord.Command
	ctx := &module.Context{
		Event:         ddiscord.Event{Type: "GUILD_CREATE", GuildID: "g1"},
		Config:        ddiscord.Config{GuildID: "g1"},
		BroadcasterID: "999",
		Log:           zap.NewNop(),
	}
	if err := handler(context.Background(), ctx, func(c ddiscord.Command) { emitted = append(emitted, c) }); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return emitted
}

// busMessage wraps a raw payload the way a lane delivery would. Context()
// defaults to Background for a Message built directly, which is what these
// tests want.
func busMessage(payload []byte) *bus.Message {
	return &bus.Message{Payload: payload}
}

func identityPayload(t *testing.T, c ddiscord.Command) ddiscord.IdentityPayload {
	t.Helper()
	var p ddiscord.IdentityPayload
	if err := codec.Unmarshal(c.Payload, &p); err != nil {
		t.Fatalf("unmarshal identity payload: %v", err)
	}
	return p
}

func TestIdentityPaidGuildGetsPremium(t *testing.T) {
	i, _ := identityFor(t, newFakeApplied(), "paid")
	cmds := runOnGuild(t, i)
	if len(cmds) != 1 {
		t.Fatalf("emitted %d commands, want 1", len(cmds))
	}
	if cmds[0].Type != ddiscord.TypeSetGuildIdentity {
		t.Fatalf("type = %q", cmds[0].Type)
	}
	if !identityPayload(t, cmds[0]).Identity.Premium {
		t.Fatal("paid guild did not get the premium identity")
	}
}

// VIP and paid are one tier by product decision. If this ever diverges it
// must be a deliberate edit here, not a silent behavior change.
func TestIdentityVIPIsTreatedAsPremium(t *testing.T) {
	i, _ := identityFor(t, newFakeApplied(), "vip")
	cmds := runOnGuild(t, i)
	if len(cmds) != 1 || !identityPayload(t, cmds[0]).Identity.Premium {
		t.Fatal("vip did not get the premium identity")
	}
}

func TestIdentityFreeGuildClearsTheOverride(t *testing.T) {
	i, _ := identityFor(t, newFakeApplied(), "free")
	cmds := runOnGuild(t, i)
	if len(cmds) != 1 {
		t.Fatalf("emitted %d commands, want 1", len(cmds))
	}
	if identityPayload(t, cmds[0]).Identity.Premium {
		t.Fatal("free guild was given the premium identity")
	}
}

// The reconnect guard. GUILD_CREATE fires for every guild on every connect,
// so without this a restart re-uploads the premium avatar fleet-wide.
func TestIdentitySecondConnectEmitsNothing(t *testing.T) {
	applied := newFakeApplied()
	i, _ := identityFor(t, applied, "paid")
	if got := len(runOnGuild(t, i)); got != 1 {
		t.Fatalf("first connect emitted %d, want 1", got)
	}
	if got := len(runOnGuild(t, i)); got != 0 {
		t.Fatalf("second connect emitted %d, want 0", got)
	}
}

func TestIdentityUpgradeAfterApplyEmitsAgain(t *testing.T) {
	applied := newFakeApplied()
	free, _ := identityFor(t, applied, "free")
	runOnGuild(t, free)
	paid, _ := identityFor(t, applied, "paid")
	cmds := runOnGuild(t, paid)
	if len(cmds) != 1 || !identityPayload(t, cmds[0]).Identity.Premium {
		t.Fatal("upgrade did not re-apply as premium")
	}
}

// An unprojected account must leave the appearance alone. Treating it as
// free would strip a paying streamer's badge every time the projection is
// briefly cold.
func TestIdentityUnknownStatusLeavesGuildAlone(t *testing.T) {
	applied := newFakeApplied()
	i, _ := identityFor(t, applied, "")
	if got := len(runOnGuild(t, i)); got != 0 {
		t.Fatalf("emitted %d commands for an unprojected account, want 0", got)
	}
	if applied.records != 0 {
		t.Fatal("recorded a fingerprint without applying anything")
	}
}

func TestIdentityUserChangedAppliesImmediately(t *testing.T) {
	applied := newFakeApplied()
	i, emitted := identityFor(t, applied, "free")
	raw, err := codec.Marshal(map[string]any{"user_id": 999, "status": "paid"})
	if err != nil {
		t.Fatalf("marshal user-changed: %v", err)
	}
	if err := i.HandleUserChanged(busMessage(raw)); err != nil {
		t.Fatalf("HandleUserChanged: %v", err)
	}
	if len(*emitted) != 1 || !identityPayload(t, (*emitted)[0]).Identity.Premium {
		t.Fatal("a tier upgrade did not publish the premium identity")
	}
}

// A malformed account event is dropped, not redelivered: other consumers
// have already handled it, and nothing about retrying it here would help.
func TestIdentityUserChangedMalformedIsAcked(t *testing.T) {
	i, emitted := identityFor(t, newFakeApplied(), "paid")
	if err := i.HandleUserChanged(busMessage([]byte("{not json"))); err != nil {
		t.Fatalf("malformed payload returned an error (would nack): %v", err)
	}
	if len(*emitted) != 0 {
		t.Fatal("malformed payload still published a command")
	}
}

// A failed Record must not swallow the apply: the command is already sent,
// so the only cost is a redundant re-apply on the next connect.
func TestIdentityRecordFailureStillEmits(t *testing.T) {
	applied := newFakeApplied()
	applied.recordErr = errors.New("valkey down")
	i, _ := identityFor(t, applied, "paid")
	if got := len(runOnGuild(t, i)); got != 1 {
		t.Fatalf("emitted %d commands, want 1", got)
	}
}

// Cosmetic work must never preempt a moderation action on the shared
// per-token budget.
func TestIdentityRidesTheDefaultLane(t *testing.T) {
	if ddiscord.ModType(ddiscord.TypeSetGuildIdentity) {
		t.Fatal("set_guild_identity is classified as a moderation command")
	}
	if got := ddiscord.Lane(ddiscord.TypeSetGuildIdentity); got != ddiscord.LaneDefault {
		t.Fatalf("lane = %q, want %q", got, ddiscord.LaneDefault)
	}
}
