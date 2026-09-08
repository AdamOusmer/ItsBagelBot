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

	"ItsBagelBot/internal/discordapi"
)

// fakeGate is a Gate that counts calls and can be told to refuse them.
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

// countingTransport stands in for the network: it counts every request that
// got past the gate and answers with an empty JSON object. Bodies that fail to
// decode are fine here -- these tests assert what reached the wire, not what
// came back.
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

// gatedClient wires a real *discordapi.Client through the gate onto a counting
// transport. It bypasses NewClient only to substitute the network; the gating
// half under test is the same value NewClient installs.
func gatedClient(gate Gate) (*discordapi.Client, *countingTransport) {
	next := &countingTransport{}
	client := discordapi.NewClient("test-token")
	client.SetTransport(gatedTransport{gate: gate, next: next})
	return client, next
}

type restCall struct {
	name string
	call func(context.Context, *discordapi.Client) error
}

// restCalls is every REST method the two Discord services call, driven through
// the real client. It is the transport-shaped heir of the old LimitedClient
// table: that one had to prove each of 34 forwarders remembered the gate, and
// a reflection sweep caught any method missing from it. Neither is a risk any
// more -- the transport gates whatever the client sends -- so this table now
// proves the weaker but sufficient thing, that these calls really do go out
// over the gated transport rather than around it.
func restCalls() []restCall {
	verification := 1
	return []restCall{
		{"SendChat", func(ctx context.Context, c *discordapi.Client) error {
			return c.SendChat(ctx, discordapi.ChatPost{})
		}},
		{"SendFile", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.SendFile(ctx, discordapi.FileUpload{})
			return err
		}},
		{"ListMessagesFull", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.ListMessagesFull(ctx, discordapi.MessagePage{})
			return err
		}},
		{"SendEmbed", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.SendEmbed(ctx, discordapi.EmbedPost{})
			return err
		}},
		{"SendPanel", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.SendPanel(ctx, discordapi.EmbedPost{}, nil)
			return err
		}},
		{"EditMessage", func(ctx context.Context, c *discordapi.Client) error {
			return c.EditMessage(ctx, discordapi.Message{}, discordapi.MessagePatch{})
		}},
		{"DeleteMessage", func(ctx context.Context, c *discordapi.Client) error {
			return c.DeleteMessage(ctx, discordapi.Message{})
		}},
		{"CreateChannel", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.CreateChannel(ctx, discordapi.GuildChannel{})
			return err
		}},
		{"DeleteChannel", func(ctx context.Context, c *discordapi.Client) error {
			return c.DeleteChannel(ctx, discordapi.Snowflake{})
		}},
		{"CreateRole", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.CreateRole(ctx, discordapi.GuildRole{})
			return err
		}},
		{"AddMemberRole", func(ctx context.Context, c *discordapi.Client) error {
			return c.AddMemberRole(ctx, discordapi.MemberRole{})
		}},
		{"ModifyCurrentMember", func(ctx context.Context, c *discordapi.Client) error {
			return c.ModifyCurrentMember(ctx, discordapi.CurrentMember{})
		}},
		{"RemoveMemberRole", func(ctx context.Context, c *discordapi.Client) error {
			return c.RemoveMemberRole(ctx, discordapi.MemberRole{})
		}},
		{"RemoveMemberRoleWithReason", func(ctx context.Context, c *discordapi.Client) error {
			return c.RemoveMemberRoleWithReason(ctx, discordapi.MemberRole{}, "raid")
		}},
		{"MoveMember", func(ctx context.Context, c *discordapi.Client) error {
			return c.MoveMember(ctx, discordapi.VoiceMove{})
		}},
		{"ModifyChannel", func(ctx context.Context, c *discordapi.Client) error {
			return c.ModifyChannel(ctx, discordapi.ChannelPatch{})
		}},
		{"TimeoutMember", func(ctx context.Context, c *discordapi.Client) error {
			return c.TimeoutMember(ctx, discordapi.MemberTimeout{})
		}},
		{"KickMember", func(ctx context.Context, c *discordapi.Client) error {
			return c.KickMember(ctx, discordapi.GuildMember{})
		}},
		{"BanMember", func(ctx context.Context, c *discordapi.Client) error {
			return c.BanMember(ctx, discordapi.GuildMember{})
		}},
		{"BulkDeleteMessages", func(ctx context.Context, c *discordapi.Client) error {
			return c.BulkDeleteMessages(ctx, discordapi.Purge{})
		}},
		{"ListMessages", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.ListMessages(ctx, discordapi.MessageQuery{})
			return err
		}},
		{"ListGuildChannels", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.ListGuildChannels(ctx, discordapi.Guild{})
			return err
		}},
		{"ListGuildRoles", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.ListGuildRoles(ctx, discordapi.Guild{})
			return err
		}},
		{"GetGuild", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.GetGuild(ctx, discordapi.Guild{})
			return err
		}},
		{"GetGuildWithCounts", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.GetGuildWithCounts(ctx, discordapi.Guild{})
			return err
		}},
		{"InteractionCallback", func(ctx context.Context, c *discordapi.Client) error {
			return c.InteractionCallback(ctx, discordapi.Callback{})
		}},
		{"InteractionFollowup", func(ctx context.Context, c *discordapi.Client) error {
			return c.InteractionFollowup(ctx, discordapi.Followup{})
		}},
		{"BulkOverwriteCommands", func(ctx context.Context, c *discordapi.Client) error {
			return c.BulkOverwriteCommands(ctx, discordapi.CommandCatalog{})
		}},
		{"GetCurrentApplication", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.GetCurrentApplication(ctx)
			return err
		}},
		{"GetInvite", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.GetInvite(ctx, "code")
			return err
		}},
		{"GetGuildMember", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.GetGuildMember(ctx, discordapi.GuildMember{})
			return err
		}},
		{"ListGuildChannelsFull", func(ctx context.Context, c *discordapi.Client) error {
			_, err := c.ListGuildChannelsFull(ctx, discordapi.Guild{})
			return err
		}},
		// ModifyGuild carries a real field because an empty patch sends no
		// request at all, which is exactly the call the old wrapper charged a
		// token for and this one does not.
		{"ModifyGuild", func(ctx context.Context, c *discordapi.Client) error {
			return c.ModifyGuild(ctx, discordapi.GuildPatch{VerificationLevel: &verification})
		}},
		{"SetChannelOverwrite", func(ctx context.Context, c *discordapi.Client) error {
			return c.SetChannelOverwrite(ctx, discordapi.ChannelOverwrite{})
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

// TestNewClientInstallsTheGate pins the wiring NewClient does, which the tests
// above bypass to substitute the network.
func TestNewClientInstallsTheGate(t *testing.T) {
	gate := &fakeGate{deny: true}
	if err := NewClient("test-token", gate).SendChat(context.Background(), discordapi.ChatPost{}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if gate.calls != 1 {
		t.Fatalf("gate calls = %d, want 1", gate.calls)
	}
}
