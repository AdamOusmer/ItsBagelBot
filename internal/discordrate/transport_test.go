// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordrate

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	api "ItsBagelBot/internal/discordapi"
)

type fakeGate struct {
	calls int
	deny  bool
}

func (g *fakeGate) Take(context.Context) error {
	g.calls++
	if g.deny {
		return ErrRateLimited
	}
	return nil
}

type countingTransport struct {
	requests int
}

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.requests++
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("{}")),
		Request:    req,
	}, nil
}

func gatedClient(gate Gate) (*api.Client, *countingTransport) {
	next := &countingTransport{}
	client := api.NewClient("test-token")
	client.SetTransport(gatedTransport{gate: gate, next: next})
	return client, next
}

type restCall struct {
	name string
	call func(context.Context, *api.Client) error
}

func ret[T any](call func(context.Context, *api.Client) (T, error)) func(context.Context, *api.Client) error {
	return func(ctx context.Context, c *api.Client) error { _, err := call(ctx, c); return err }
}

func restCalls() []restCall {
	level := 1
	return []restCall{
		{"SendChat", func(ctx context.Context, c *api.Client) error { return c.SendChat(ctx, api.ChatPost{}) }},
		{"SendFile", ret(func(ctx context.Context, c *api.Client) (api.Message, error) {
			return c.SendFile(ctx, api.FileUpload{})
		})},
		{"ListMessagesFull", ret(func(ctx context.Context, c *api.Client) ([]api.FullMessage, error) {
			return c.ListMessagesFull(ctx, api.MessagePage{})
		})},
		{"SendEmbed", ret(func(ctx context.Context, c *api.Client) (api.Message, error) {
			return c.SendEmbed(ctx, api.EmbedPost{})
		})},
		{"SendPanel", ret(func(ctx context.Context, c *api.Client) (api.Message, error) {
			return c.SendPanel(ctx, api.EmbedPost{}, nil)
		})},
		{"EditMessage", func(ctx context.Context, c *api.Client) error {
			return c.EditMessage(ctx, api.Message{}, api.MessagePatch{})
		}},
		{"DeleteMessage", func(ctx context.Context, c *api.Client) error { return c.DeleteMessage(ctx, api.Message{}) }},
		{"CreateChannel", ret(func(ctx context.Context, c *api.Client) (api.Snowflake, error) {
			return c.CreateChannel(ctx, api.GuildChannel{})
		})},
		{"DeleteChannel", func(ctx context.Context, c *api.Client) error { return c.DeleteChannel(ctx, api.Snowflake{}) }},
		{"CreateRole", ret(func(ctx context.Context, c *api.Client) (api.Snowflake, error) {
			return c.CreateRole(ctx, api.GuildRole{})
		})},
		{"AddMemberRole", func(ctx context.Context, c *api.Client) error { return c.AddMemberRole(ctx, api.MemberRole{}) }},
		{"ModifyCurrentMember", func(ctx context.Context, c *api.Client) error { return c.ModifyCurrentMember(ctx, api.CurrentMember{}) }},
		{"RemoveMemberRole", func(ctx context.Context, c *api.Client) error { return c.RemoveMemberRole(ctx, api.MemberRole{}) }},
		{"RemoveMemberRoleWithReason", func(ctx context.Context, c *api.Client) error {
			return c.RemoveMemberRoleWithReason(ctx, api.MemberRole{}, "raid")
		}},
		{"MoveMember", func(ctx context.Context, c *api.Client) error { return c.MoveMember(ctx, api.VoiceMove{}) }},
		{"ModifyChannel", func(ctx context.Context, c *api.Client) error { return c.ModifyChannel(ctx, api.ChannelPatch{}) }},
		{"TimeoutMember", func(ctx context.Context, c *api.Client) error { return c.TimeoutMember(ctx, api.MemberTimeout{}) }},
		{"KickMember", func(ctx context.Context, c *api.Client) error { return c.KickMember(ctx, api.GuildMember{}) }},
		{"BanMember", func(ctx context.Context, c *api.Client) error { return c.BanMember(ctx, api.GuildMember{}) }},
		{"BulkDeleteMessages", func(ctx context.Context, c *api.Client) error { return c.BulkDeleteMessages(ctx, api.Purge{}) }},
		{"ListMessages", ret(func(ctx context.Context, c *api.Client) ([]api.Snowflake, error) {
			return c.ListMessages(ctx, api.MessageQuery{})
		})},
		{"ListGuildChannels", ret(func(ctx context.Context, c *api.Client) ([]api.Snowflake, error) {
			return c.ListGuildChannels(ctx, api.Guild{})
		})},
		{"ListGuildRoles", ret(func(ctx context.Context, c *api.Client) ([]api.Snowflake, error) {
			return c.ListGuildRoles(ctx, api.Guild{})
		})},
		{"GetGuild", ret(func(ctx context.Context, c *api.Client) (api.Snowflake, error) { return c.GetGuild(ctx, api.Guild{}) })},
		{"GetGuildWithCounts", ret(func(ctx context.Context, c *api.Client) (api.GuildInfo, error) {
			return c.GetGuildWithCounts(ctx, api.Guild{})
		})},
		{"InteractionCallback", func(ctx context.Context, c *api.Client) error { return c.InteractionCallback(ctx, api.Callback{}) }},
		{"InteractionFollowup", func(ctx context.Context, c *api.Client) error { return c.InteractionFollowup(ctx, api.Followup{}) }},
		{"BulkOverwriteCommands", func(ctx context.Context, c *api.Client) error {
			return c.BulkOverwriteCommands(ctx, api.CommandCatalog{})
		}},
		{"GetCurrentApplication", ret(func(ctx context.Context, c *api.Client) (api.Snowflake, error) { return c.GetCurrentApplication(ctx) })},
		{"GetInvite", ret(func(ctx context.Context, c *api.Client) (api.Invite, error) { return c.GetInvite(ctx, "code") })},
		{"GetGuildMember", ret(func(ctx context.Context, c *api.Client) (api.GuildMemberInfo, error) {
			return c.GetGuildMember(ctx, api.GuildMember{})
		})},
		{"ListGuildChannelsFull", ret(func(ctx context.Context, c *api.Client) ([]api.ChannelInfo, error) {
			return c.ListGuildChannelsFull(ctx, api.Guild{})
		})},
		{"ModifyGuild", func(ctx context.Context, c *api.Client) error {
			return c.ModifyGuild(ctx, api.GuildPatch{VerificationLevel: &level})
		}},
		{"SetChannelOverwrite", func(ctx context.Context, c *api.Client) error {
			return c.SetChannelOverwrite(ctx, api.ChannelOverwrite{})
		}},
	}
}

func TestEveryRestCallPaysOneToken(t *testing.T) {
	for _, tc := range restCalls() {
		t.Run(tc.name, func(t *testing.T) {
			gate := &fakeGate{}
			client, next := gatedClient(gate)
			_ = tc.call(context.Background(), client)
			if gate.calls != 1 || next.requests != 1 {
				t.Fatalf("gate calls = %d, requests sent = %d, want 1 and 1", gate.calls, next.requests)
			}
		})
	}
}

func TestRefusedCallNeverReachesDiscord(t *testing.T) {
	for _, tc := range restCalls() {
		t.Run(tc.name, func(t *testing.T) {
			gate := &fakeGate{deny: true}
			client, next := gatedClient(gate)
			if err := tc.call(context.Background(), client); !errors.Is(err, ErrRateLimited) {
				t.Fatalf("err = %v, want ErrRateLimited", err)
			}
			if next.requests != 0 {
				t.Fatal("a refused call must never reach Discord")
			}
		})
	}
}

func TestNewClientInstallsTheGate(t *testing.T) {
	gate := &fakeGate{deny: true}
	if err := NewClient("test-token", gate).SendChat(context.Background(), api.ChatPost{}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if gate.calls != 1 {
		t.Fatalf("gate calls = %d, want 1", gate.calls)
	}
}
