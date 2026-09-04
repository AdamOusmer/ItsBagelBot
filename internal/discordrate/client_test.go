// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordrate

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/internal/discordapi"
)

// fakeGate is a Gate that counts calls and can be told to refuse the next one.
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

// fakeRest records every call it receives; LimitedClient must never reach it
// when the gate refuses.
type fakeRest struct {
	sends int
}

func (f *fakeRest) SendChat(context.Context, discordapi.ChatPost) error { f.sends++; return nil }
func (f *fakeRest) SendEmbed(context.Context, discordapi.EmbedPost) (discordapi.Message, error) {
	f.sends++
	return discordapi.Message{ID: "m1"}, nil
}
func (f *fakeRest) SendPanel(context.Context, discordapi.EmbedPost, []discordapi.Button) (discordapi.Message, error) {
	f.sends++
	return discordapi.Message{ID: "p1"}, nil
}
func (f *fakeRest) DeleteMessage(context.Context, discordapi.Message) error { f.sends++; return nil }
func (f *fakeRest) EditMessage(context.Context, discordapi.Message, discordapi.MessagePatch) error {
	f.sends++
	return nil
}
func (f *fakeRest) CreateChannel(context.Context, discordapi.GuildChannel) (discordapi.Snowflake, error) {
	f.sends++
	return discordapi.Snowflake{ID: "c1"}, nil
}
func (f *fakeRest) DeleteChannel(context.Context, discordapi.Snowflake) error { f.sends++; return nil }
func (f *fakeRest) CreateRole(context.Context, discordapi.GuildRole) (discordapi.Snowflake, error) {
	f.sends++
	return discordapi.Snowflake{ID: "r1"}, nil
}
func (f *fakeRest) AddMemberRole(context.Context, discordapi.MemberRole) error { f.sends++; return nil }
func (f *fakeRest) ModifyCurrentMember(context.Context, discordapi.CurrentMember) error {
	f.sends++
	return nil
}
func (f *fakeRest) RemoveMemberRole(context.Context, discordapi.MemberRole) error {
	f.sends++
	return nil
}
func (f *fakeRest) MoveMember(context.Context, discordapi.VoiceMove) error { f.sends++; return nil }
func (f *fakeRest) ModifyChannel(context.Context, discordapi.ChannelPatch) error {
	f.sends++
	return nil
}
func (f *fakeRest) TimeoutMember(context.Context, discordapi.MemberTimeout) error {
	f.sends++
	return nil
}
func (f *fakeRest) KickMember(context.Context, discordapi.GuildMember) error   { f.sends++; return nil }
func (f *fakeRest) BanMember(context.Context, discordapi.GuildMember) error    { f.sends++; return nil }
func (f *fakeRest) BulkDeleteMessages(context.Context, discordapi.Purge) error { f.sends++; return nil }
func (f *fakeRest) ListMessages(context.Context, discordapi.MessageQuery) ([]discordapi.Snowflake, error) {
	f.sends++
	return nil, nil
}
func (f *fakeRest) ListGuildChannels(context.Context, discordapi.Guild) ([]discordapi.Snowflake, error) {
	f.sends++
	return nil, nil
}
func (f *fakeRest) ListGuildRoles(context.Context, discordapi.Guild) ([]discordapi.Snowflake, error) {
	f.sends++
	return nil, nil
}
func (f *fakeRest) GetGuild(context.Context, discordapi.Guild) (discordapi.Snowflake, error) {
	f.sends++
	return discordapi.Snowflake{ID: "g1"}, nil
}
func (f *fakeRest) InteractionCallback(context.Context, discordapi.Callback) error {
	f.sends++
	return nil
}
func (f *fakeRest) BulkOverwriteCommands(context.Context, discordapi.CommandCatalog) error {
	f.sends++
	return nil
}
func (f *fakeRest) InteractionFollowup(context.Context, discordapi.Followup) error {
	f.sends++
	return nil
}
func (f *fakeRest) GetCurrentApplication(context.Context) (discordapi.Snowflake, error) {
	f.sends++
	return discordapi.Snowflake{}, nil
}
func (f *fakeRest) GetInvite(context.Context, string) (discordapi.Invite, error) {
	f.sends++
	return discordapi.Invite{}, nil
}

var _ rest = (*fakeRest)(nil)

func TestLimitedClientPaysGateBeforeSending(t *testing.T) {
	gate := &fakeGate{}
	underlying := &fakeRest{}
	c := NewLimitedClient(underlying, gate)

	if err := c.SendChat(context.Background(), discordapi.ChatPost{ChannelID: "1", Content: "hi"}); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	if gate.calls != 1 {
		t.Fatalf("gate calls = %d, want 1", gate.calls)
	}
	if underlying.sends != 1 {
		t.Fatalf("underlying sends = %d, want 1", underlying.sends)
	}
}

func TestLimitedClientRefusesWithoutCallingRest(t *testing.T) {
	gate := &fakeGate{deny: true}
	underlying := &fakeRest{}
	c := NewLimitedClient(underlying, gate)

	err := c.SendChat(context.Background(), discordapi.ChatPost{ChannelID: "1", Content: "hi"})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if underlying.sends != 0 {
		t.Fatal("a refused call must never reach the REST client")
	}
}

// TestLimitedClientGatesEveryMethod is a table test rather than one function
// per method: 21 near-identical "call X, assert the gate paid" cases would
// otherwise be 21 near-identical test functions.
func TestLimitedClientGatesEveryMethod(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		call func(*LimitedClient) error
	}{
		{"SendEmbed", func(c *LimitedClient) error { _, err := c.SendEmbed(ctx, discordapi.EmbedPost{}); return err }},
		{"SendPanel", func(c *LimitedClient) error { _, err := c.SendPanel(ctx, discordapi.EmbedPost{}, nil); return err }},
		{"EditMessage", func(c *LimitedClient) error {
			return c.EditMessage(ctx, discordapi.Message{}, discordapi.MessagePatch{})
		}},
		{"DeleteMessage", func(c *LimitedClient) error { return c.DeleteMessage(ctx, discordapi.Message{}) }},
		{"CreateChannel", func(c *LimitedClient) error { _, err := c.CreateChannel(ctx, discordapi.GuildChannel{}); return err }},
		{"DeleteChannel", func(c *LimitedClient) error { return c.DeleteChannel(ctx, discordapi.Snowflake{}) }},
		{"CreateRole", func(c *LimitedClient) error { _, err := c.CreateRole(ctx, discordapi.GuildRole{}); return err }},
		{"AddMemberRole", func(c *LimitedClient) error { return c.AddMemberRole(ctx, discordapi.MemberRole{}) }},
		{"ModifyCurrentMember", func(c *LimitedClient) error { return c.ModifyCurrentMember(ctx, discordapi.CurrentMember{}) }},
		{"RemoveMemberRole", func(c *LimitedClient) error { return c.RemoveMemberRole(ctx, discordapi.MemberRole{}) }},
		{"MoveMember", func(c *LimitedClient) error { return c.MoveMember(ctx, discordapi.VoiceMove{}) }},
		{"ModifyChannel", func(c *LimitedClient) error { return c.ModifyChannel(ctx, discordapi.ChannelPatch{}) }},
		{"TimeoutMember", func(c *LimitedClient) error { return c.TimeoutMember(ctx, discordapi.MemberTimeout{}) }},
		{"KickMember", func(c *LimitedClient) error { return c.KickMember(ctx, discordapi.GuildMember{}) }},
		{"BanMember", func(c *LimitedClient) error { return c.BanMember(ctx, discordapi.GuildMember{}) }},
		{"BulkDeleteMessages", func(c *LimitedClient) error { return c.BulkDeleteMessages(ctx, discordapi.Purge{}) }},
		{"ListMessages", func(c *LimitedClient) error { _, err := c.ListMessages(ctx, discordapi.MessageQuery{}); return err }},
		{"ListGuildChannels", func(c *LimitedClient) error { _, err := c.ListGuildChannels(ctx, discordapi.Guild{}); return err }},
		{"ListGuildRoles", func(c *LimitedClient) error { _, err := c.ListGuildRoles(ctx, discordapi.Guild{}); return err }},
		{"GetGuild", func(c *LimitedClient) error { _, err := c.GetGuild(ctx, discordapi.Guild{}); return err }},
		{"InteractionCallback", func(c *LimitedClient) error { return c.InteractionCallback(ctx, discordapi.Callback{}) }},
		{"BulkOverwriteCommands", func(c *LimitedClient) error { return c.BulkOverwriteCommands(ctx, discordapi.CommandCatalog{}) }},
		{"InteractionFollowup", func(c *LimitedClient) error { return c.InteractionFollowup(ctx, discordapi.Followup{}) }},
		{"GetCurrentApplication", func(c *LimitedClient) error { _, err := c.GetCurrentApplication(ctx); return err }},
		{"GetInvite", func(c *LimitedClient) error { _, err := c.GetInvite(ctx, "code"); return err }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gate := &fakeGate{deny: true}
			c := NewLimitedClient(&fakeRest{}, gate)
			if err := tc.call(c); !errors.Is(err, ErrRateLimited) {
				t.Fatalf("%s: err = %v, want ErrRateLimited", tc.name, err)
			}
			if gate.calls != 1 {
				t.Fatalf("%s: gate calls = %d, want 1", tc.name, gate.calls)
			}
		})
	}
}
