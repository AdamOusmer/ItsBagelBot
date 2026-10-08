// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "ItsBagelBot/internal/discordapi"
)

type apiCall func(context.Context, *api.Client) error

func sendMessage(ctx context.Context, c *api.Client) error {
	return c.SendMessage(ctx, "1234", "hello", false)
}

func getGuild(ctx context.Context, c *api.Client) error {
	_, err := c.GetGuildWithCounts(ctx, api.Guild{ID: "9"})
	return err
}

func getMember(ctx context.Context, c *api.Client) error {
	_, err := c.GetGuildMember(ctx, api.GuildMember{GuildID: "g1", UserID: "u1"})
	return err
}

func sendFile(ctx context.Context, c *api.Client) error {
	_, err := c.SendFile(ctx, api.FileUpload{ChannelID: "c1", Filename: "t.txt", Data: []byte("x")})
	return err
}

var errTransient = errors.New("unclassified, so the worker nacks and retries")

func classOf(err error) error {
	if err == nil {
		return nil
	}
	known := []error{api.ErrBadRequest, api.ErrAuth, api.ErrForbidden, api.ErrChannelNotFound, api.ErrRateLimited, api.ErrNoMessageID}
	if i := slices.IndexFunc(known, func(e error) bool { return errors.Is(err, e) }); i >= 0 {
		return known[i]
	}
	return errTransient
}

type classified struct {
	name   string
	call   apiCall
	status int
	reply  string
	want   error
	retry  time.Duration
}

func TestResponsesAreClassified(t *testing.T) {
	for _, tc := range []classified{
		{"a 2xx is a success", sendMessage, 200, `{}`, nil, 0},
		{"a bad request is permanent", sendMessage, 400, `{"message":"Cannot send an empty message"}`, api.ErrBadRequest, 0},
		{"an unauthorized token is permanent", sendMessage, 401, `{}`, api.ErrAuth, 0},
		{"a forbidden post is permanent", sendMessage, 403, `{"message":"Missing Permissions"}`, api.ErrForbidden, 0},
		{"an unknown channel is permanent", sendMessage, 404, `{"message":"Unknown Channel"}`, api.ErrChannelNotFound, 0},
		{"a rate limit carries its retry delay", sendMessage, 429, `{"retry_after":1.5}`, api.ErrRateLimited, 1500 * time.Millisecond},
		{"a server error stays transient", sendMessage, 502, `{}`, errTransient, 0},
		{"a forbidden guild lookup is classified", getGuild, 403, `{"message":"Missing Access"}`, api.ErrForbidden, 0},
		{"an unknown member reads as not found", getMember, 404, `{"message":"Unknown Member"}`, api.ErrChannelNotFound, 0},
		{"an undecodable 2xx body is an error", getMember, 200, `not json`, errTransient, 0},
		{"a forbidden upload is classified", sendFile, 403, `{"message":"Missing Permissions"}`, api.ErrForbidden, 0},
		{"an upload answered without a message id is refused", sendFile, 200, `{}`, api.ErrNoMessageID, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := fakeDiscord(tc.status, tc.reply)
			err := tc.call(context.Background(), client)
			assert.Equal(t, tc.want, classOf(err), "err = %v", err)
			assert.Equal(t, tc.retry, api.RetryAfterOf(err))
		})
	}
}

type endpoint struct {
	name  string
	reply string
	call  func(context.Context, *api.Client) (any, error)
	sent  sentRequest
	want  any
}

func TestEndpointsRoundTrip(t *testing.T) {
	for _, tc := range slices.Concat(messageEndpoints(), guildEndpoints(), channelEndpoints()) {
		t.Run(tc.name, func(t *testing.T) {
			client, discord := fakeDiscord(http.StatusOK, tc.reply)
			got, err := tc.call(context.Background(), client)
			require.NoError(t, err)
			assert.Equal(t, tc.sent, discord.sent)
			assert.Equal(t, tc.want, got)
		})
	}
}

type messageView struct {
	ID          string
	Author      string
	At          string
	Attachments []api.MessageAttachment
}

func viewMessages(page []api.FullMessage, err error) (any, error) {
	var out []messageView
	for _, m := range page {
		at := m.At().Format(time.RFC3339)
		out = append(out, messageView{ID: m.ID, Author: m.Author.DisplayName(), At: at, Attachments: m.Attachments})
	}
	return out, err
}

func messageEndpoints() []endpoint {
	page := func(p api.MessagePage) func(context.Context, *api.Client) (any, error) {
		return func(ctx context.Context, c *api.Client) (any, error) { return viewMessages(c.ListMessagesFull(ctx, p)) }
	}
	return []endpoint{{
		name:  "a message carries its content and tts flag",
		reply: `{}`,
		call: func(ctx context.Context, c *api.Client) (any, error) {
			return nil, c.SendMessage(ctx, "1234567890", "hello", true)
		},
		sent: sentRequest{Method: "POST", URI: "/channels/1234567890/messages", Body: `{"content":"hello","tts":true}`},
	}, {
		name:  "a chat post keeps tts off the wire",
		reply: `{}`,
		call: func(ctx context.Context, c *api.Client) (any, error) {
			return nil, c.SendChat(ctx, api.ChatPost{ChannelID: "1", Content: "hi"})
		},
		sent: sentRequest{Method: "POST", URI: "/channels/1/messages", Body: `{"content":"hi"}`},
	}, {
		name: "a message page clamps its limit and decodes authors, attachments and timestamps",
		reply: `[
		  {"id":"m2","content":"bye","timestamp":"2026-01-02T03:04:05.000000+00:00",
		   "author":{"id":"u1","username":"ada","global_name":"Ada L"},
		   "attachments":[{"url":"https://cdn/x.png","filename":"x.png"}]},
		  {"id":"m1","content":"hi","timestamp":"not-a-time","author":{"id":"u2","username":"bob"}},
		  {"id":"m0","author":{"id":"u3"}}
		]`,
		call: page(api.MessagePage{ChannelID: "c1", Before: "m3", Limit: 250}),
		sent: sentRequest{Method: "GET", URI: "/channels/c1/messages?before=m3&limit=100"},
		want: []messageView{
			{ID: "m2", Author: "Ada L", At: "2026-01-02T03:04:05Z",
				Attachments: []api.MessageAttachment{{URL: "https://cdn/x.png", Filename: "x.png"}}},
			{ID: "m1", Author: "bob", At: "0001-01-01T00:00:00Z"},
			{ID: "m0", Author: "u3", At: "0001-01-01T00:00:00Z"},
		},
	}, {
		name:  "a message page without a limit asks for the maximum",
		reply: `[]`,
		call:  page(api.MessagePage{ChannelID: "c1"}),
		sent:  sentRequest{Method: "GET", URI: "/channels/c1/messages?limit=100"},
		want:  []messageView(nil),
	}, {
		name:  "a message page under the maximum keeps its limit and cursor",
		reply: `[]`,
		call:  page(api.MessagePage{ChannelID: "c1", Limit: 20, Before: "m9"}),
		sent:  sentRequest{Method: "GET", URI: "/channels/c1/messages?before=m9&limit=20"},
		want:  []messageView(nil),
	}}
}

type guildView struct {
	Info    api.GuildInfo
	IconURL string
}

func viewGuild(g api.GuildInfo, err error) (any, error) {
	return guildView{Info: g, IconURL: g.IconURL()}, err
}

func guildEndpoints() []endpoint {
	level := api.GuildVerificationHighest
	withCounts := func(ctx context.Context, c *api.Client) (any, error) {
		return viewGuild(c.GetGuildWithCounts(ctx, api.Guild{ID: "9"}))
	}
	return []endpoint{{
		name:  "a guild with counts decodes its icon url",
		reply: `{"id":"9","name":"Bagel HQ","icon":"abc","approximate_member_count":1234}`,
		call:  withCounts,
		sent:  sentRequest{Method: "GET", URI: "/guilds/9?with_counts=true"},
		want: guildView{
			Info:    api.GuildInfo{ID: "9", Name: "Bagel HQ", Icon: "abc", ApproximateMemberCount: 1234},
			IconURL: "https://cdn.discordapp.com/icons/9/abc.png",
		},
	}, {
		name:  "a guild without an icon has no icon url, so the dashboard renders its placeholder",
		reply: `{"id":"9","name":"Bagel HQ"}`,
		call:  withCounts,
		sent:  sentRequest{Method: "GET", URI: "/guilds/9?with_counts=true"},
		want:  guildView{Info: api.GuildInfo{ID: "9", Name: "Bagel HQ"}},
	}, {
		name:  "guild roles keep the managed flag StripRoles skips on",
		reply: `[{"id":"r1","name":"Mods"},{"id":"r2","name":"Bagel","managed":true}]`,
		call: func(ctx context.Context, c *api.Client) (any, error) {
			return c.ListGuildRoles(ctx, api.Guild{ID: "g1"})
		},
		sent: sentRequest{Method: "GET", URI: "/guilds/g1/roles"},
		want: []api.Snowflake{{ID: "r1", Name: "Mods"}, {ID: "r2", Name: "Bagel", Managed: true}},
	}, {
		name:  "a verification patch sends the level",
		reply: `{}`,
		call: func(ctx context.Context, c *api.Client) (any, error) {
			return nil, c.ModifyGuild(ctx, api.GuildPatch{Guild: api.Guild{ID: "g1"}, VerificationLevel: &level})
		},
		sent: sentRequest{Method: "PATCH", URI: "/guilds/g1", Body: `{"verification_level":4}`},
	}, {
		name:  "an empty guild patch sends nothing",
		reply: `{}`,
		call: func(ctx context.Context, c *api.Client) (any, error) {
			return nil, c.ModifyGuild(ctx, api.GuildPatch{Guild: api.Guild{ID: "g1"}})
		},
	}, {
		name:  "a guild member decodes roles and user",
		reply: `{"nick":"Bagelfan","roles":["r1","r2"],"user":{"id":"u1","username":"fan","global_name":"Fan"}}`,
		call: func(ctx context.Context, c *api.Client) (any, error) {
			return c.GetGuildMember(ctx, api.GuildMember{GuildID: "g1", UserID: "u1"})
		},
		sent: sentRequest{Method: "GET", URI: "/guilds/g1/members/u1"},
		want: api.GuildMemberInfo{
			Roles: []string{"r1", "r2"}, Nick: "Bagelfan",
			User: api.MemberUser{ID: "u1", Username: "fan", GlobalName: "Fan"},
		},
	}}
}

func channelEndpoints() []endpoint {
	empty, target := "", "cat1"
	rename := func(parent *string) func(context.Context, *api.Client) (any, error) {
		return func(ctx context.Context, c *api.Client) (any, error) {
			return nil, c.ModifyChannel(ctx, api.ChannelPatch{ID: "c1", Name: "closed-ticket-ada-1", ParentID: parent})
		}
	}
	return []endpoint{{
		name: "guild channels keep their parent and overwrites",
		reply: `[{"id":"c1","name":"chat","type":0,"parent_id":"cat1",
			"permission_overwrites":[{"id":"g1","type":0,"allow":"1024","deny":"0"}]}]`,
		call: func(ctx context.Context, c *api.Client) (any, error) {
			return c.ListGuildChannelsFull(ctx, api.Guild{ID: "g1"})
		},
		sent: sentRequest{Method: "GET", URI: "/guilds/g1/channels"},
		want: []api.ChannelInfo{{
			ID: "c1", Name: "chat", ParentID: "cat1",
			PermissionOverwrites: []api.PermissionOverwrite{{ID: "g1", Allow: "1024", Deny: "0"}},
		}},
	}, {
		name:  "an overwrite targets the overwrite endpoint",
		reply: `{}`,
		call: func(ctx context.Context, c *api.Client) (any, error) {
			return nil, c.SetChannelOverwrite(ctx, api.ChannelOverwrite{
				ChannelID: "c1", Overwrite: api.PermissionOverwrite{ID: "g1", Allow: "0", Deny: "2048"},
			})
		},
		sent: sentRequest{Method: "PUT", URI: "/channels/c1/permissions/g1", Body: `{"allow":"0","deny":"2048","id":"g1","type":0}`},
	}, {
		name:  "a nil parent leaves the category alone",
		reply: `{}`,
		call:  rename(nil),
		sent:  sentRequest{Method: "PATCH", URI: "/channels/c1", Body: `{"name":"closed-ticket-ada-1"}`},
	}, {
		name:  "an empty parent moves out of every category",
		reply: `{}`,
		call:  rename(&empty),
		sent:  sentRequest{Method: "PATCH", URI: "/channels/c1", Body: `{"name":"closed-ticket-ada-1","parent_id":null}`},
	}, {
		name:  "a parent id moves the channel under it",
		reply: `{}`,
		call:  rename(&target),
		sent:  sentRequest{Method: "PATCH", URI: "/channels/c1", Body: `{"name":"closed-ticket-ada-1","parent_id":"cat1"}`},
	}}
}

func TestRemoveMemberRoleWithReasonEncodesTheHeader(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		want   string
	}{
		{"plain", "raid response", "raid%20response"},
		{"non ascii", "raid réponse", "raid%20r%C3%A9ponse"},
		{"header injection attempt", "a\nX-Evil: 1", "a%0AX-Evil:%201"},
		{"empty sends no header", "", ""},
		{"TestAuditReasonTruncatesToDiscordsCap", strings.Repeat("a", 512+50), strings.Repeat("a", 512)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, discord := fakeDiscord(http.StatusNoContent, "")

			err := client.RemoveMemberRoleWithReason(context.Background(),
				api.MemberRole{GuildID: "g1", UserID: "u1", RoleID: "r1"}, tc.reason)

			require.NoError(t, err)
			assert.Equal(t, tc.want, discord.header.Get("X-Audit-Log-Reason"))
			assert.Equal(t, sentRequest{Method: "DELETE", URI: "/guilds/g1/members/u1/roles/r1"}, discord.sent)
		})
	}
}
