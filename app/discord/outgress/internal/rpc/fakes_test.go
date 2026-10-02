// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/discord/outgress/internal/kv"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type recordedCall struct {
	method      string
	path        string
	query       string
	body        string
	contentType string
}

func (c recordedCall) label() string {
	out := c.method + " " + c.path
	if c.query != "" {
		out += "?" + c.query
	}
	if strings.HasPrefix(c.contentType, "multipart/") {
		out += " multipart"
	}
	return out
}

type answer struct {
	status int
	body   string
	err    error
}

type scriptedTransport struct {
	channelGuilds map[string]string
	routes        map[string]answer
	mu            sync.Mutex
	calls         []recordedCall
	reply         func(call recordedCall) (int, string)
}

func (s *scriptedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	call := recordedCall{
		method: r.Method, path: strings.TrimPrefix(r.URL.Path, "/api/v10"),
		query: r.URL.RawQuery, contentType: r.Header.Get("Content-Type"),
	}
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		call.body = string(raw)
	}
	s.mu.Lock()
	s.calls = append(s.calls, call)
	s.mu.Unlock()

	got := s.answerFor(call)
	if got.err != nil {
		return nil, got.err
	}
	rec := httptest.NewRecorder()
	rec.Code = got.status
	rec.Body.WriteString(got.body)
	return rec.Result(), nil
}

func (s *scriptedTransport) answerFor(call recordedCall) answer {
	if id, lookup := channelLookupID(call); lookup {
		return answer{status: http.StatusOK, body: fmt.Sprintf(`{"id":%q,"guild_id":%q}`, id, s.guildOf(id))}
	}
	if routed, ok := s.routes[call.label()]; ok {
		return routed
	}
	if s.reply != nil {
		status, body := s.reply(call)
		return answer{status: status, body: body}
	}
	return answer{status: http.StatusOK, body: `{"id":"m-new"}`}
}

func (s *scriptedTransport) guildOf(channelID string) string {
	if s.channelGuilds == nil {
		return "g1"
	}
	return s.channelGuilds[channelID]
}

func (s *scriptedTransport) find(method, path string) []recordedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []recordedCall
	for _, c := range s.calls {
		if c.method == method && c.path == path {
			out = append(out, c)
		}
	}
	return out
}

func (s *scriptedTransport) indexOf(method, path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.calls {
		if c.method == method && c.path == path {
			return i
		}
	}
	return -1
}

func (s *scriptedTransport) trail() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, c := range s.calls {
		out = append(out, c.label())
	}
	return out
}

func (s *scriptedTransport) bodyOf(label string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls {
		if c.label() == label {
			return c.body
		}
	}
	return ""
}

func channelLookupID(call recordedCall) (string, bool) {
	if call.method != http.MethodGet {
		return "", false
	}
	if !strings.HasPrefix(call.path, "/channels/") {
		return "", false
	}
	id := strings.TrimPrefix(call.path, "/channels/")
	return id, !strings.Contains(id, "/")
}

func discordClient(tr *scriptedTransport) *discapi.Client {
	client := discapi.NewClient("bot-token")
	client.SetTransport(tr)
	return client
}

func newTicketRPC(t *testing.T, reply func(recordedCall) (int, string)) (*ticketRPC, *scriptedTransport) {
	t.Helper()
	tr := &scriptedTransport{reply: reply}
	return &ticketRPC{rest: discordClient(tr), botID: "bot9", log: zap.NewNop()}, tr
}

type onceMemo struct{ claimed map[int]bool }

func newOnceMemo() *onceMemo { return &onceMemo{claimed: map[int]bool{}} }

func (m *onceMemo) ClaimSummary(_ context.Context, ticketID int) bool {
	if ticketID <= 0 {
		return true
	}
	if m.claimed[ticketID] {
		return false
	}
	m.claimed[ticketID] = true
	return true
}

type fakeEngineREST struct {
	channelGuilds map[string]string

	deleted  []string
	modified []discapi.ChannelPatch
	listed   []discapi.Snowflake
	bulkDel  []discapi.Purge
	sent     []discapi.EmbedPost
	edited   []discapi.Message

	inviteCodes []string
	inviteReply discapi.Invite
	inviteErr   error
}

func (f *fakeEngineREST) GetChannel(_ context.Context, id string) (discapi.ChannelInfo, error) {
	guild := "g1"
	if f.channelGuilds != nil {
		guild = f.channelGuilds[id]
	}
	return discapi.ChannelInfo{ID: id, GuildID: guild}, nil
}

func (f *fakeEngineREST) CreateChannel(_ context.Context, ch discapi.GuildChannel) (discapi.Snowflake, error) {
	return discapi.Snowflake{ID: "ch-" + ch.Spec.Name}, nil
}

func (f *fakeEngineREST) DeleteChannel(_ context.Context, ch discapi.Snowflake) error {
	f.deleted = append(f.deleted, ch.ID)
	return nil
}

func (f *fakeEngineREST) ModifyChannel(_ context.Context, patch discapi.ChannelPatch) error {
	f.modified = append(f.modified, patch)
	return nil
}

func (f *fakeEngineREST) MoveMember(context.Context, discapi.VoiceMove) error { return nil }

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

func (m *memLive) snapshot() map[kv.GuildID]discapi.Message {
	if len(m.msgs) == 0 {
		return nil
	}
	return maps.Clone(m.msgs)
}

type deskPanel struct {
	Channel string
	Title   string
	Body    string
	Color   int
	Button  string
}

type fakeSetupREST struct {
	channelGuild string

	channels []discapi.Snowflake
	roles    []discapi.Snowflake
	member   discapi.GuildMemberInfo
	guild    discapi.GuildInfo
	guildErr error

	desks []deskPanel
	chats []discapi.ChatPost
}

func (f *fakeSetupREST) GetChannel(_ context.Context, id string) (discapi.ChannelInfo, error) {
	guild := f.channelGuild
	if guild == "" {
		guild = "g1"
	}
	return discapi.ChannelInfo{ID: id, GuildID: guild}, nil
}

func (f *fakeSetupREST) SendChat(_ context.Context, post discapi.ChatPost) error {
	f.chats = append(f.chats, post)
	return nil
}

func (f *fakeSetupREST) DeleteMessage(context.Context, discapi.Message) error { return nil }

func (f *fakeSetupREST) SendPanel(_ context.Context, post discapi.EmbedPost, buttons []discapi.Button) (discapi.Message, error) {
	f.desks = append(f.desks, deskPanel{
		Channel: post.ChannelID, Title: post.Embed.Title, Body: post.Embed.Description,
		Color: post.Embed.Color, Button: buttons[0].Label,
	})
	return discapi.Message{ChannelID: post.ChannelID, ID: "m-new"}, nil
}

func (f *fakeSetupREST) CreateChannel(context.Context, discapi.GuildChannel) (discapi.Snowflake, error) {
	return discapi.Snowflake{}, nil
}

func (f *fakeSetupREST) CreateRole(context.Context, discapi.GuildRole) (discapi.Snowflake, error) {
	return discapi.Snowflake{}, nil
}

func (f *fakeSetupREST) ListGuildChannels(context.Context, discapi.Guild) ([]discapi.Snowflake, error) {
	return f.channels, nil
}

func (f *fakeSetupREST) ListGuildRoles(context.Context, discapi.Guild) ([]discapi.Snowflake, error) {
	if f.roles == nil {
		return []discapi.Snowflake{{ID: "role-1", Name: "@everyone"}}, nil
	}
	return f.roles, nil
}

func (f *fakeSetupREST) GetGuildMember(context.Context, discapi.GuildMember) (discapi.GuildMemberInfo, error) {
	return f.member, nil
}

func (f *fakeSetupREST) GetGuildWithCounts(context.Context, discapi.Guild) (discapi.GuildInfo, error) {
	return f.guild, f.guildErr
}

type fakeBotStatus struct {
	st ddiscord.BotStatus
	ok bool
}

func (f fakeBotStatus) BotStatus(context.Context) (ddiscord.BotStatus, bool) { return f.st, f.ok }

type flaggedReauth map[string]bool

func (f flaggedReauth) NeedsReauth(_ context.Context, g kv.GuildID) bool { return f[string(g)] }

type unavailableStore struct{ *discordstore.Mem }

func (unavailableStore) GuildsOf(context.Context, discordstore.Broadcaster) ([]discordstore.Binding, error) {
	return nil, discordstore.ErrStoreUnavailable
}

func rpcWiring(t *testing.T) Wiring {
	t.Helper()
	return Wiring{NC: testnats.Connect(t), Prefix: "test.discord", Queue: "discord", Log: zap.NewNop()}
}

func requestRPC(t *testing.T, wire Wiring, verb string, req any) []byte {
	t.Helper()
	body, err := codec.Marshal(req)
	require.NoError(t, err)
	msg, err := wire.NC.Request(wire.Prefix+"."+verb, body, 2*time.Second)
	require.NoError(t, err)
	return msg.Data
}

func decodeAs(t *testing.T, raw []byte, like any) any {
	t.Helper()
	out := reflect.New(reflect.TypeOf(like))
	require.NoError(t, codec.Unmarshal(raw, out.Interface()))
	return out.Elem().Interface()
}

func decodeJSON(t *testing.T, raw string) any {
	t.Helper()
	if raw == "" {
		return nil
	}
	var out any
	require.NoError(t, json.Unmarshal([]byte(raw), &out), raw)
	return out
}

type discordServer func(rest *discapi.Client, live kv.LiveStore, wire Wiring) error

type discordCase struct {
	name    string
	verb    string
	req     any
	routes  map[string]answer
	script  func(recordedCall) (int, string)
	live    map[kv.GuildID]discapi.Message
	want    any
	calls   []string
	write   string
	body    string
	tracked map[kv.GuildID]discapi.Message
}

type discordExchange struct {
	Reply   any
	Calls   []string
	Body    any
	Tracked map[kv.GuildID]discapi.Message
}

func (tc discordCase) expected(t *testing.T) discordExchange {
	return discordExchange{Reply: tc.want, Calls: tc.calls, Body: decodeJSON(t, tc.body), Tracked: tc.tracked}
}

func (tc discordCase) exchange(t *testing.T, serve discordServer) discordExchange {
	got, _ := tc.exchangeWith(t, serve)
	return got
}

func (tc discordCase) exchangeWith(t *testing.T, serve discordServer) (discordExchange, *scriptedTransport) {
	tr := &scriptedTransport{routes: tc.routes, reply: tc.script}
	live := &memLive{msgs: maps.Clone(tc.live)}
	if live.msgs == nil {
		live.msgs = map[kv.GuildID]discapi.Message{}
	}
	wire := rpcWiring(t)
	require.NoError(t, serve(discordClient(tr), live, wire))
	reply := decodeAs(t, requestRPC(t, wire, tc.verb, tc.req), tc.want)
	return discordExchange{
		Reply: reply, Calls: tr.trail(), Body: decodeJSON(t, tr.bodyOf(tc.write)), Tracked: live.snapshot(),
	}, tr
}
