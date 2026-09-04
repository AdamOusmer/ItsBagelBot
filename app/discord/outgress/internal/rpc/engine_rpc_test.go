// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/kv"
	discapi "ItsBagelBot/internal/discordapi"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"

	"go.uber.org/zap"
)

type fakeEngineREST struct {
	created  []string
	deleted  []string
	modified []discapi.ChannelPatch
	moved    []discapi.VoiceMove
	listed   []discapi.Snowflake
	bulkDel  []discapi.Purge
	sent     []discapi.EmbedPost
	edited   []discapi.Message

	inviteCodes []string
	inviteReply discapi.Invite
	inviteErr   error
}

func (f *fakeEngineREST) CreateChannel(_ context.Context, ch discapi.GuildChannel) (discapi.Snowflake, error) {
	id := "ch-" + ch.Spec.Name
	f.created = append(f.created, id)
	return discapi.Snowflake{ID: id}, nil
}
func (f *fakeEngineREST) DeleteChannel(_ context.Context, ch discapi.Snowflake) error {
	f.deleted = append(f.deleted, ch.ID)
	return nil
}
func (f *fakeEngineREST) ModifyChannel(_ context.Context, patch discapi.ChannelPatch) error {
	f.modified = append(f.modified, patch)
	return nil
}
func (f *fakeEngineREST) MoveMember(_ context.Context, move discapi.VoiceMove) error {
	f.moved = append(f.moved, move)
	return nil
}
func (f *fakeEngineREST) ListMessages(context.Context, discapi.MessageQuery) ([]discapi.Snowflake, error) {
	return f.listed, nil
}
func (f *fakeEngineREST) BulkDeleteMessages(_ context.Context, p discapi.Purge) error {
	f.bulkDel = append(f.bulkDel, p)
	return nil
}
func (f *fakeEngineREST) SendEmbed(_ context.Context, post discapi.EmbedPost) (discapi.Message, error) {
	f.sent = append(f.sent, post)
	return discapi.Message{ChannelID: post.ChannelID, ID: "msg-1"}, nil
}
func (f *fakeEngineREST) EditMessage(_ context.Context, m discapi.Message, _ discapi.MessagePatch) error {
	f.edited = append(f.edited, m)
	return nil
}
func (f *fakeEngineREST) GetInvite(_ context.Context, code string) (discapi.Invite, error) {
	f.inviteCodes = append(f.inviteCodes, code)
	return f.inviteReply, f.inviteErr
}

type memLive struct {
	msgs map[kv.GuildID]discapi.Message
}

func newMemLive() *memLive { return &memLive{msgs: map[kv.GuildID]discapi.Message{}} }

func (m *memLive) PutLiveMessage(_ context.Context, guildID kv.GuildID, msg discapi.Message) error {
	m.msgs[guildID] = msg
	return nil
}
func (m *memLive) GetLiveMessage(_ context.Context, guildID kv.GuildID) (discapi.Message, bool) {
	msg, ok := m.msgs[guildID]
	return msg, ok
}
func (m *memLive) DeleteLiveMessage(_ context.Context, guildID kv.GuildID) error {
	delete(m.msgs, guildID)
	return nil
}

func TestHandleCreateReturnsTheChannelID(t *testing.T) {
	rest := &fakeEngineREST{}
	h := &engineRPC{rest: rest, log: zap.NewNop()}
	reply := h.handleCreate(context.Background(), discordoutgress.ChannelCreateRequest{GuildID: "g1", Name: "ticket-ada"})
	if reply.Error != "" {
		t.Fatalf("error = %s", reply.Error)
	}
	if reply.ChannelID != "ch-ticket-ada" {
		t.Fatalf("channel id = %s", reply.ChannelID)
	}
}

func TestHandlePurgeBelowMinimumStillReportsCount(t *testing.T) {
	rest := &fakeEngineREST{listed: []discapi.Snowflake{{ID: "m1"}}}
	h := &engineRPC{rest: rest, log: zap.NewNop()}
	reply := h.handlePurge(context.Background(), discordoutgress.PurgeRequest{ChannelID: "c1", Count: 50})
	if reply.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (below Discord's 2-message minimum, no bulk-delete call)", reply.Deleted)
	}
	if len(rest.bulkDel) != 0 {
		t.Fatal("must not call bulk-delete under the minimum")
	}
}

func TestHandlePurgeBulkDeletes(t *testing.T) {
	rest := &fakeEngineREST{listed: []discapi.Snowflake{{ID: "m1"}, {ID: "m2"}, {ID: "m3"}}}
	h := &engineRPC{rest: rest, log: zap.NewNop()}
	reply := h.handlePurge(context.Background(), discordoutgress.PurgeRequest{ChannelID: "c1", Count: 50})
	if reply.Deleted != 3 {
		t.Fatalf("deleted = %d, want 3", reply.Deleted)
	}
	if len(rest.bulkDel) != 1 || len(rest.bulkDel[0].MessageIDs) != 3 {
		t.Fatalf("bulk delete = %+v", rest.bulkDel)
	}
}

func TestHandleLiveOnlineIsIdempotentPerStream(t *testing.T) {
	rest := &fakeEngineREST{}
	live := newMemLive()
	h := &engineRPC{rest: rest, live: live, log: zap.NewNop()}

	first := h.handleLiveOnline(context.Background(), discordoutgress.LiveOnlineRequest{GuildID: "g1", ChannelID: "c1"})
	if first.Error != "" {
		t.Fatalf("first online: %s", first.Error)
	}
	if len(rest.sent) != 1 {
		t.Fatalf("expected exactly one embed sent, got %d", len(rest.sent))
	}

	// A repeat call for the same (still-announced) stream must not post a
	// second embed -- this is the whole reason LiveOnline is an RPC and not
	// a fire-and-forget Command (see internal/domain/rpc/discordoutgress's doc).
	second := h.handleLiveOnline(context.Background(), discordoutgress.LiveOnlineRequest{GuildID: "g1", ChannelID: "c1"})
	if second.Error != "" {
		t.Fatalf("second online: %s", second.Error)
	}
	if len(rest.sent) != 1 {
		t.Fatalf("repeat go-live must not post again, got %d sends", len(rest.sent))
	}
}

func TestHandleLiveOfflineEditsAndForgets(t *testing.T) {
	rest := &fakeEngineREST{}
	live := newMemLive()
	h := &engineRPC{rest: rest, live: live, log: zap.NewNop()}

	_ = h.handleLiveOnline(context.Background(), discordoutgress.LiveOnlineRequest{GuildID: "g1", ChannelID: "c1"})
	reply := h.handleLiveOffline(context.Background(), discordoutgress.LiveOfflineRequest{GuildID: "g1"})
	if reply.Error != "" {
		t.Fatalf("offline: %s", reply.Error)
	}
	if len(rest.edited) != 1 {
		t.Fatalf("expected the go-live message to be edited, got %d edits", len(rest.edited))
	}
	if _, known := live.GetLiveMessage(context.Background(), "g1"); known {
		t.Fatal("the live message must be forgotten after the offline edit")
	}
}

func TestHandleLiveOfflineWithNoKnownMessageIsANoOp(t *testing.T) {
	rest := &fakeEngineREST{}
	h := &engineRPC{rest: rest, live: newMemLive(), log: zap.NewNop()}
	reply := h.handleLiveOffline(context.Background(), discordoutgress.LiveOfflineRequest{GuildID: "g1"})
	if reply.Error != "" {
		t.Fatalf("offline: %s", reply.Error)
	}
	if len(rest.edited) != 0 {
		t.Fatal("must not edit anything when no go-live message is known")
	}
}

func TestHandleInviteResolveReturnsTheTargetGuild(t *testing.T) {
	rest := &fakeEngineREST{inviteReply: discapi.Invite{Code: "abc", Guild: &discapi.Snowflake{ID: "g1"}}}
	h := &engineRPC{rest: rest, log: zap.NewNop()}
	reply := h.handleInviteResolve(context.Background(), discordoutgress.InviteResolveRequest{Code: "abc"})
	if reply.Error != "" || reply.NotFound {
		t.Fatalf("reply = %+v, want a resolved guild", reply)
	}
	if reply.GuildID != "g1" {
		t.Fatalf("guild id = %q, want g1", reply.GuildID)
	}
	if len(rest.inviteCodes) != 1 || rest.inviteCodes[0] != "abc" {
		t.Fatalf("GetInvite called with %v, want [abc]", rest.inviteCodes)
	}
}

// TestHandleInviteResolve404IsNotFoundNotError proves a dead code (revoked,
// expired, never existed) is reported as NotFound rather than Error --
// engine caches NotFound but never caches Error (see InviteResolveReply's
// doc), and a dead invite is exactly the amplification case (a spam wave of
// junk codes) that caching needs to hold.
func TestHandleInviteResolve404IsNotFoundNotError(t *testing.T) {
	rest := &fakeEngineREST{inviteErr: discapi.ErrChannelNotFound}
	h := &engineRPC{rest: rest, log: zap.NewNop()}
	reply := h.handleInviteResolve(context.Background(), discordoutgress.InviteResolveRequest{Code: "dead"})
	if !reply.NotFound || reply.Error != "" {
		t.Fatalf("reply = %+v, want NotFound with no Error", reply)
	}
}

// TestHandleInviteResolveGroupDMInviteIsNotFound proves an invite that
// resolved successfully but named no guild (a group-DM invite -- discord.gg
// codes are not guild-exclusive) also collapses to NotFound: linkguard's
// only question is "does this code target guild X", and a codeless-of-guild
// invite can never answer yes.
func TestHandleInviteResolveGroupDMInviteIsNotFound(t *testing.T) {
	rest := &fakeEngineREST{inviteReply: discapi.Invite{Code: "dm1"}}
	h := &engineRPC{rest: rest, log: zap.NewNop()}
	reply := h.handleInviteResolve(context.Background(), discordoutgress.InviteResolveRequest{Code: "dm1"})
	if !reply.NotFound || reply.GuildID != "" {
		t.Fatalf("reply = %+v, want NotFound with no guild id", reply)
	}
}

// TestHandleInviteResolveTransientErrorIsError proves an unclassified
// failure (network, 5xx, the shared rate-limit bucket) comes back as Error,
// not NotFound -- engine must not cache this as a confirmed answer (see
// linkguard.go's tripIsOwnInvite doc for the fail-safe this distinction
// drives).
func TestHandleInviteResolveTransientErrorIsError(t *testing.T) {
	rest := &fakeEngineREST{inviteErr: errors.New("boom")}
	h := &engineRPC{rest: rest, log: zap.NewNop()}
	reply := h.handleInviteResolve(context.Background(), discordoutgress.InviteResolveRequest{Code: "x"})
	if reply.Error == "" || reply.NotFound {
		t.Fatalf("reply = %+v, want a non-empty Error and NotFound false", reply)
	}
}
