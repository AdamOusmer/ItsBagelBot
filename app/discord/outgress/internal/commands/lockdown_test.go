// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
	"errors"
	"slices"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/kv"
	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

// fakeLockdowns is an in-memory kv.LockdownStore.
type fakeLockdowns struct {
	state   map[string]kv.LockdownState
	putErr  error
	deleted []string
}

func newFakeLockdowns() *fakeLockdowns {
	return &fakeLockdowns{state: map[string]kv.LockdownState{}}
}

func (f *fakeLockdowns) PutLockdown(_ context.Context, g kv.GuildID, s kv.LockdownState) error {
	if f.putErr != nil {
		return f.putErr
	}
	f.state[string(g)] = s
	return nil
}

func (f *fakeLockdowns) GetLockdown(_ context.Context, g kv.GuildID) (kv.LockdownState, bool) {
	s, ok := f.state[string(g)]
	return s, ok
}

func (f *fakeLockdowns) DeleteLockdown(_ context.Context, g kv.GuildID) error {
	f.deleted = append(f.deleted, string(g))
	delete(f.state, string(g))
	return nil
}

// lockdownRest is a guild whose cat1 holds one channel of each type a mute
// means something for, plus a voice channel and a channel in another
// category that must be left alone.
func lockdownRest() *fakeRest {
	return &fakeRest{
		guild: discapi.Snowflake{ID: "g1", VerificationLevel: 1},
		fullChannels: []discapi.ChannelInfo{
			{ID: "c-text", Type: ddiscord.ChannelText, ParentID: "cat1", PermissionOverwrites: []discapi.PermissionOverwrite{
				{ID: "g1", Type: 0, Allow: "1024", Deny: "0"},
			}},
			{ID: "c-news", Type: ddiscord.ChannelNews, ParentID: "cat1"},
			{ID: "c-forum", Type: ddiscord.ChannelForum, ParentID: "cat1"},
			{ID: "c-voice", Type: ddiscord.ChannelVoice, ParentID: "cat1"},
			{ID: "c-other", Type: ddiscord.ChannelText, ParentID: "cat2"},
		},
	}
}

func lockdownCommand(t *testing.T, categories ...string) ddiscord.Command {
	t.Helper()
	return ddiscord.Command{
		Type: ddiscord.TypeLockdown, GuildID: "g1",
		Payload: marshalPayload(t, ddiscord.LockdownPayload{CategoryIDs: categories}),
	}
}

func mutedChannels(rest *fakeRest) []string {
	var got []string
	for _, o := range rest.overwrites {
		got = append(got, o.ChannelID)
	}
	return got
}

// lockdownHandlers is the wiring every lockdown and unlock case needs: the
// fake REST, the test logger, and the undo store when the case has one. The
// three cases that pass no store are asserting the half of a lockdown that
// happens without one, so a nil store stays a supported shape here.
func lockdownHandlers(rest *fakeRest, store *fakeLockdowns) *Handlers {
	h := &Handlers{Rest: rest, Log: testLogger()}
	if store != nil {
		h.Lockdown = store
	}
	return h
}

// A lockdown with no categories is still the useful half: the verification
// bump is what stops new accounts at the door.
func TestLockdownRaisesVerificationLevel(t *testing.T) {
	rest := &fakeRest{}
	h := lockdownHandlers(rest, nil)

	dispatchOK(t, h, ddiscord.Command{Type: ddiscord.TypeLockdown, GuildID: "g1"})

	if len(rest.guildPatches) != 1 {
		t.Fatalf("guild patches = %d, want 1", len(rest.guildPatches))
	}
	got := rest.guildPatches[0]
	if got.VerificationLevel == nil || *got.VerificationLevel != discapi.GuildVerificationHighest {
		t.Fatalf("verification level = %v, want %d", got.VerificationLevel, discapi.GuildVerificationHighest)
	}
	if len(rest.overwrites) != 0 {
		t.Fatalf("overwrites = %d, want 0 with no categories", len(rest.overwrites))
	}
}

// Muting must ADD the SEND deny to whatever the channel already denies and
// clear it from the allow, because Discord's overwrite write replaces the
// whole overwrite -- a bare deny would drop the VIEW allow that makes a
// gated channel visible to its tier.
func TestLockdownMutesTextChannelsInCategoriesOnly(t *testing.T) {
	rest := &fakeRest{fullChannels: []discapi.ChannelInfo{
		{ID: "c-text", Type: ddiscord.ChannelText, ParentID: "cat1", PermissionOverwrites: []discapi.PermissionOverwrite{
			{ID: "g1", Type: 0, Allow: "1024", Deny: "0"},
		}},
		{ID: "c-voice", Type: ddiscord.ChannelVoice, ParentID: "cat1"},
		{ID: "c-other", Type: ddiscord.ChannelText, ParentID: "cat2"},
	}}
	h := lockdownHandlers(rest, nil)

	dispatchOK(t, h, ddiscord.Command{
		Type: ddiscord.TypeLockdown, GuildID: "g1",
		Payload: marshalPayload(t, ddiscord.LockdownPayload{CategoryIDs: []string{"cat1"}}),
	})

	if len(rest.overwrites) != 1 {
		t.Fatalf("overwrites = %d, want 1", len(rest.overwrites))
	}
	got := rest.overwrites[0]
	if got.ChannelID != "c-text" {
		t.Fatalf("channel = %q, want c-text", got.ChannelID)
	}
	if got.Overwrite.ID != "g1" {
		t.Fatalf("overwrite target = %q, want the guild id (@everyone)", got.Overwrite.ID)
	}
	if got.Overwrite.Allow != "1024" || got.Overwrite.Deny != "2048" {
		t.Fatalf("allow/deny = %q/%q, want 1024/2048", got.Overwrite.Allow, got.Overwrite.Deny)
	}
}

// A failed lockdown must NACK. The old no-op ACKed the message, so a
// lockdown the bot lacked MANAGE_GUILD for looked delivered.
func TestLockdownSurfacesOverwriteFailure(t *testing.T) {
	rest := &fakeRest{
		overwriteErr: errors.New("403 missing permissions"),
		fullChannels: []discapi.ChannelInfo{{ID: "c1", Type: ddiscord.ChannelText, ParentID: "cat1"}},
	}
	h := lockdownHandlers(rest, nil)

	err := h.Dispatch(context.Background(), ddiscord.Command{
		Type: ddiscord.TypeLockdown, GuildID: "g1",
		Payload: marshalPayload(t, ddiscord.LockdownPayload{CategoryIDs: []string{"cat1"}}),
	})
	if err == nil {
		t.Fatal("a refused overwrite must surface so the lane redelivers")
	}
}

// Announcement and forum channels are places members post, so a lockdown
// that muted only type 0 left the loudest surfaces of a modern server open.
// Voice carries SPEAK rather than SEND, so writing there changes nothing.
func TestLockdownMutesEveryPostableChannelType(t *testing.T) {
	rest := lockdownRest()
	h := lockdownHandlers(rest, newFakeLockdowns())

	dispatchOK(t, h, lockdownCommand(t, "cat1"))

	want := []string{"c-text", "c-news", "c-forum"}
	if got := mutedChannels(rest); !slices.Equal(got, want) {
		t.Fatalf("muted = %v, want %v", got, want)
	}
}

// The undo state must be written BEFORE anything changes: once @everyone is
// denied SEND, nothing on Discord's side still says whether that deny was
// the streamer's own.
func TestLockdownRecordsTheStateItIsAboutToDisplace(t *testing.T) {
	rest := lockdownRest()
	store := newFakeLockdowns()
	h := lockdownHandlers(rest, store)

	dispatchOK(t, h, lockdownCommand(t, "cat1"))

	state, ok := store.state["g1"]
	if !ok {
		t.Fatal("lockdown recorded no undo state")
	}
	if state.VerificationLevel != 1 {
		t.Fatalf("stored level = %d, want the level in force before the bump (1)", state.VerificationLevel)
	}
	if len(state.Channels) != 3 {
		t.Fatalf("stored channels = %d, want 3", len(state.Channels))
	}
	first := state.Channels[0]
	if first.ChannelID != "c-text" || first.Allow != "1024" || first.Deny != "0" {
		t.Fatalf("stored channel = %+v, want c-text with its prior 1024/0", first)
	}
	// A channel with no @everyone overwrite is remembered as an explicit
	// zero pair, which restores permission-identically.
	if state.Channels[1].Allow != "0" || state.Channels[1].Deny != "0" {
		t.Fatalf("stored channel = %+v, want a zero pair", state.Channels[1])
	}
}

// The verification bump is the half that stops NEW accounts. Its failure
// must return before any channel is touched, or a retry of the whole
// lockdown finds the door still open.
func TestLockdownShortCircuitsOnModifyGuildFailure(t *testing.T) {
	rest := lockdownRest()
	rest.guildPatchErrs = []error{discapi.ErrForbidden}
	h := lockdownHandlers(rest, newFakeLockdowns())

	err := h.Dispatch(context.Background(), lockdownCommand(t, "cat1"))
	if !errors.Is(err, discapi.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if len(rest.overwrites) != 0 {
		t.Fatalf("muted %v after the verification bump failed", mutedChannels(rest))
	}
}

// Every channel is attempted and every failure is reported: one unwritable
// channel must not leave the rest of the guild open, and the joined error
// still nacks so the lane redelivers the idempotent mute.
func TestLockdownAggregatesPerChannelFailures(t *testing.T) {
	rest := lockdownRest()
	rest.overwriteErr = discapi.ErrRateLimited
	h := lockdownHandlers(rest, newFakeLockdowns())

	err := h.Dispatch(context.Background(), lockdownCommand(t, "cat1"))
	if !errors.Is(err, discapi.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if len(rest.overwrites) != 3 {
		t.Fatalf("attempted = %d channels, want all 3 even after the first failed", len(rest.overwrites))
	}
}

// Unparseable allow bits used to read as zero, which rewrote that channel's
// @everyone allow to nothing -- dropping a VIEW allow and hiding the channel
// from the whole server as a side effect of a lockdown. Now it fails that
// channel and the others still go through.
func TestLockdownFailsOnlyTheChannelWithUnparseableBits(t *testing.T) {
	rest := lockdownRest()
	rest.fullChannels[0].PermissionOverwrites[0].Allow = "not-a-bitfield"
	h := lockdownHandlers(rest, newFakeLockdowns())

	err := h.Dispatch(context.Background(), lockdownCommand(t, "cat1"))
	if err == nil {
		t.Fatal("an unreadable overwrite must surface, not silently zero the allow bits")
	}
	want := []string{"c-news", "c-forum"}
	if got := mutedChannels(rest); !slices.Equal(got, want) {
		t.Fatalf("muted = %v, want the other channels still muted %v", got, want)
	}
}

// Unlock is the whole point of recording the state: level back, overwrites
// back verbatim, key gone.
func TestUnlockRestoresLevelAndOverwrites(t *testing.T) {
	rest := lockdownRest()
	store := newFakeLockdowns()
	h := lockdownHandlers(rest, store)

	dispatchOK(t, h, lockdownCommand(t, "cat1"))
	rest.overwrites = nil
	rest.guildPatches = nil

	dispatchOK(t, h, ddiscord.Command{Type: ddiscord.TypeUnlock, GuildID: "g1"})

	if len(rest.guildPatches) != 1 {
		t.Fatalf("guild patches = %d, want 1", len(rest.guildPatches))
	}
	if lvl := rest.guildPatches[0].VerificationLevel; lvl == nil || *lvl != 1 {
		t.Fatalf("restored level = %v, want 1", lvl)
	}
	want := []string{"c-text", "c-news", "c-forum"}
	if got := mutedChannels(rest); !slices.Equal(got, want) {
		t.Fatalf("restored = %v, want %v", got, want)
	}
	if rest.overwrites[0].Overwrite.Allow != "1024" || rest.overwrites[0].Overwrite.Deny != "0" {
		t.Fatalf("restored overwrite = %+v, want the prior 1024/0", rest.overwrites[0].Overwrite)
	}
	if !slices.Equal(store.deleted, []string{"g1"}) {
		t.Fatalf("deleted = %v, want the guild's key dropped once", store.deleted)
	}
}

// No remembered state is TERMINAL, not an error: the key expired or the
// lockdown predates the store, and no redelivery conjures the old
// overwrites back.
func TestUnlockWithoutStateIsTerminal(t *testing.T) {
	rest := lockdownRest()
	h := lockdownHandlers(rest, newFakeLockdowns())

	dispatchOK(t, h, ddiscord.Command{Type: ddiscord.TypeUnlock, GuildID: "g1"})

	if len(rest.guildPatches) != 0 || len(rest.overwrites) != 0 {
		t.Fatal("unlock touched the guild with nothing remembered")
	}
}

// A refused restore must nack: a channel left muted is a channel nobody can
// talk in, and the write is idempotent.
func TestUnlockSurfacesRestoreFailure(t *testing.T) {
	rest := lockdownRest()
	store := newFakeLockdowns()
	h := lockdownHandlers(rest, store)
	dispatchOK(t, h, lockdownCommand(t, "cat1"))
	rest.overwriteErr = discapi.ErrRateLimited

	err := h.Dispatch(context.Background(), ddiscord.Command{Type: ddiscord.TypeUnlock, GuildID: "g1"})
	if !errors.Is(err, discapi.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if len(store.deleted) != 0 {
		t.Fatal("the undo state must survive a failed restore")
	}
}

// A lockdown must still happen when its undo state cannot be written:
// an unliftable lockdown beats a raid that was never stopped.
func TestLockdownProceedsWhenTheStoreFails(t *testing.T) {
	rest := lockdownRest()
	store := newFakeLockdowns()
	store.putErr = errors.New("valkey unreachable")
	h := lockdownHandlers(rest, store)

	dispatchOK(t, h, lockdownCommand(t, "cat1"))

	if len(rest.guildPatches) != 1 || len(rest.overwrites) != 3 {
		t.Fatalf("patches = %d, mutes = %d, want 1 and 3", len(rest.guildPatches), len(rest.overwrites))
	}
}
