// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands

import (
	"context"
	"errors"
	"slices"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

func stripRest() *fakeRest {
	return &fakeRest{
		members: map[string]discapi.GuildMemberInfo{
			"u1":  {Roles: []string{"g1", "r-mod", "r-boost", "r-vip", "r-admin"}},
			"bot": {Roles: []string{"r-bot"}},
		},
		guildRoles: []discapi.Snowflake{
			{ID: "r-mod", Position: 3},
			{ID: "r-boost", Position: 4, Managed: true},
			{ID: "r-vip", Position: 2},
			{ID: "r-bot", Position: 9, Managed: true},
			{ID: "r-admin", Position: 10},
		},
	}
}

func stripHandlers(rest *fakeRest) *Handlers {
	return &Handlers{Rest: rest, ApplicationID: "bot", Log: testLogger()}
}

func removedRoles(rest *fakeRest) []string {
	var got []string
	for _, r := range rest.roleRems {
		got = append(got, r.RoleID)
	}
	return got
}

func stripCommand(reason string) ddiscord.Command {
	return ddiscord.Command{Type: ddiscord.TypeStripRoles, GuildID: "g1", UserID: "u1", Reason: reason}
}

func runStrip(rest *fakeRest, reason string) error {
	return stripHandlers(rest).Dispatch(context.Background(), stripCommand(reason))
}

func runStripOK(t *testing.T, rest *fakeRest, reason string) {
	t.Helper()
	if err := runStrip(rest, reason); err != nil {
		t.Fatalf("dispatch strip: %v", err)
	}
}

func TestStripRolesSkipsManagedEveryoneAndHigherRoles(t *testing.T) {
	rest := stripRest()

	runStripOK(t, rest, "")

	want := []string{"r-mod", "r-vip"}
	if got := removedRoles(rest); !slices.Equal(got, want) {
		t.Fatalf("removed = %v, want %v", got, want)
	}
}

func TestStripRolesCarriesTheAuditReason(t *testing.T) {
	rest := stripRest()

	runStripOK(t, rest, "raid response")

	for _, got := range rest.roleRemReasons {
		if got != "raid response" {
			t.Fatalf("audit reason = %q, want %q", got, "raid response")
		}
	}
	if len(rest.roleRemReasons) != 2 {
		t.Fatalf("removals = %d, want 2", len(rest.roleRemReasons))
	}
}

func TestStripRolesSkipsForbiddenRoleAndContinues(t *testing.T) {
	rest := stripRest()
	rest.removeErrs = map[string]error{"r-mod": discapi.ErrForbidden}

	runStripOK(t, rest, "")

	want := []string{"r-vip"}
	if got := removedRoles(rest); !slices.Equal(got, want) {
		t.Fatalf("removed = %v, want %v", got, want)
	}
}

func TestStripRolesReturnsRetryableFailure(t *testing.T) {
	rest := stripRest()
	rest.removeErrs = map[string]error{"r-mod": discapi.ErrRateLimited}

	err := runStrip(rest, "")
	if !errors.Is(err, discapi.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if got := removedRoles(rest); !slices.Equal(got, []string{"r-vip"}) {
		t.Fatalf("removed = %v, want the remaining role", got)
	}
}

func TestStripRolesFailsWhenLookupsFail(t *testing.T) {
	cases := []struct {
		name string
		hurt func(*fakeRest)
	}{
		{"member fetch", func(f *fakeRest) { f.memberErr = discapi.ErrChannelNotFound }},
		{"role list", func(f *fakeRest) { f.guildRolesErr = discapi.ErrForbidden }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest := stripRest()
			tc.hurt(rest)

			err := runStrip(rest, "")
			if err == nil {
				t.Fatal("a failed lookup must nack, not silently strip nothing")
			}
			if len(rest.roleRems) != 0 {
				t.Fatalf("removed %v after a failed lookup", removedRoles(rest))
			}
		})
	}
}
