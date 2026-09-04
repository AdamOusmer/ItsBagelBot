// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// apiPrefix is the versioned API root every path carries; the tests below
// pin the endpoint, not the version.
const apiPrefix = "/api/v10"

// capture records the one request a call made, so a test can pin the method
// and path (a wrong path is a 404 nobody notices until production).
type capture struct {
	method string
	path   string
	body   string
}

func recording(t *testing.T, status int, reply string) (*Client, *capture) {
	t.Helper()
	got := &capture{}
	client := NewClient("bot-token")
	client.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got.method = r.Method
		got.path = strings.TrimPrefix(r.URL.Path, apiPrefix)
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			got.body = string(raw)
		}
		return jsonResponse(status, reply), nil
	}))
	return client, got
}

func TestGetGuildMemberDecodesRolesAndUser(t *testing.T) {
	client, got := recording(t, 200, `{"nick":"Bagelfan","roles":["r1","r2"],"user":{"id":"u1","username":"fan","global_name":"Fan"}}`)

	member, err := client.GetGuildMember(context.Background(), GuildMember{GuildID: "g1", UserID: "u1"})
	if err != nil {
		t.Fatalf("GetGuildMember: %v", err)
	}
	if got.method != http.MethodGet || got.path != "/guilds/g1/members/u1" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	if len(member.Roles) != 2 || member.Roles[0] != "r1" {
		t.Fatalf("roles = %v", member.Roles)
	}
	if member.Nick != "Bagelfan" || member.User.GlobalName != "Fan" {
		t.Fatalf("member = %+v", member)
	}
}

func TestGetGuildMemberClassifiesMissingMember(t *testing.T) {
	client, _ := recording(t, 404, `{"message": "Unknown Member"}`)

	if _, err := client.GetGuildMember(context.Background(), GuildMember{GuildID: "g1", UserID: "u1"}); err == nil {
		t.Fatal("a 404 must surface as an error")
	}
}

func TestListGuildChannelsFullKeepsParentAndOverwrites(t *testing.T) {
	client, got := recording(t, 200, `[{"id":"c1","name":"chat","type":0,"parent_id":"cat1",
		"permission_overwrites":[{"id":"g1","type":0,"allow":"1024","deny":"0"}]}]`)

	channels, err := client.ListGuildChannelsFull(context.Background(), Guild{ID: "g1"})
	if err != nil {
		t.Fatalf("ListGuildChannelsFull: %v", err)
	}
	if got.path != "/guilds/g1/channels" {
		t.Fatalf("path = %s", got.path)
	}
	if len(channels) != 1 || channels[0].ParentID != "cat1" {
		t.Fatalf("channels = %+v", channels)
	}
	if len(channels[0].PermissionOverwrites) != 1 || channels[0].PermissionOverwrites[0].Allow != "1024" {
		t.Fatalf("overwrites = %+v", channels[0].PermissionOverwrites)
	}
}

// ListGuildRoles has to carry "managed" now, because StripRoles skips those.
func TestListGuildRolesDecodesManaged(t *testing.T) {
	client, _ := recording(t, 200, `[{"id":"r1","name":"Mods"},{"id":"r2","name":"Bagel","managed":true}]`)

	roles, err := client.ListGuildRoles(context.Background(), Guild{ID: "g1"})
	if err != nil {
		t.Fatalf("ListGuildRoles: %v", err)
	}
	if len(roles) != 2 || roles[0].Managed || !roles[1].Managed {
		t.Fatalf("roles = %+v", roles)
	}
}

func TestModifyGuildSendsVerificationLevel(t *testing.T) {
	client, got := recording(t, 200, `{}`)
	level := GuildVerificationHighest

	if err := client.ModifyGuild(context.Background(), GuildPatch{Guild: Guild{ID: "g1"}, VerificationLevel: &level}); err != nil {
		t.Fatalf("ModifyGuild: %v", err)
	}
	if got.method != http.MethodPatch || got.path != "/guilds/g1" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	if !strings.Contains(got.body, `"verification_level":4`) {
		t.Fatalf("body = %s", got.body)
	}
}

// An empty patch must not reach Discord: PATCH /guilds with an empty body is
// a wasted token off a bucket a lockdown is racing.
func TestModifyGuildEmptyPatchSendsNothing(t *testing.T) {
	client, got := recording(t, 200, `{}`)

	if err := client.ModifyGuild(context.Background(), GuildPatch{Guild: Guild{ID: "g1"}}); err != nil {
		t.Fatalf("ModifyGuild: %v", err)
	}
	if got.method != "" {
		t.Fatalf("an empty patch sent %s %s", got.method, got.path)
	}
}

func TestSetChannelOverwriteTargetsTheOverwriteEndpoint(t *testing.T) {
	client, got := recording(t, 200, `{}`)

	err := client.SetChannelOverwrite(context.Background(), ChannelOverwrite{
		ChannelID: "c1", Overwrite: PermissionOverwrite{ID: "g1", Type: 0, Allow: "0", Deny: "2048"},
	})
	if err != nil {
		t.Fatalf("SetChannelOverwrite: %v", err)
	}
	if got.method != http.MethodPut || got.path != "/channels/c1/permissions/g1" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	if !strings.Contains(got.body, `"deny":"2048"`) {
		t.Fatalf("body = %s", got.body)
	}
}
