// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
)

type fakeFollowage struct {
	result engine.FollowageResult
	err    error
	got    struct{ broadcasterID, targetID, targetLogin string }
}

func (f *fakeFollowage) Lookup(_ context.Context, broadcasterID, targetID, targetLogin string) (engine.FollowageResult, error) {
	f.got = struct{ broadcasterID, targetID, targetLogin string }{broadcasterID, targetID, targetLogin}
	return f.result, f.err
}

func lookupContext() *module.Context {
	return &module.Context{Env: lane.Envelope{
		BroadcasterUserID: "5", ChatterUserID: "9", ChatterUserLogin: "viewer", ChatterUserName: "Viewer",
	}, BroadcasterID: 5}
}

func disabledModule(name string) []projection.ModuleView {
	return []projection.ModuleView{{Name: name, IsEnabled: false}}
}

func TestFollowageAndAccountAgeReplies(t *testing.T) {
	following := engine.FollowageResult{TargetID: "9", UserFound: true, Following: true, FollowedAt: time.Now().Add(-40 * 24 * time.Hour)}
	oldAccount := engine.AccountAgeResult{TargetID: "9", UserFound: true, CreatedAt: time.Now().Add(-400 * 24 * time.Hour)}
	boom := errors.New("boom")
	cases := []struct {
		name       string
		text       string
		followage  engine.FollowageResult
		followErr  error
		accountAge engine.AccountAgeResult
		ageErr     error
		modules    []projection.ModuleView
		want       []string
	}{
		{name: "followage defaults to the chatter", text: "!followage", followage: following,
			want: []string{"@Viewer has followed for 1 month, 10 days."}},
		{name: "accountage defaults to the chatter", text: "!accountage", accountAge: oldAccount,
			want: []string{"@Viewer's account is 1 year, 1 month old."}},
		{name: "followage accepts a target login", text: "!followage @Other ignored",
			followage: engine.FollowageResult{TargetID: "10", UserFound: true},
			want:      []string{"@Other is not following this channel."}},
		{name: "accountage accepts a target login", text: "!accountage @Ghost ignored",
			want: []string{"@Ghost is not a Twitch user."}},
		{name: "followage lookup failure replies unavailable", text: "!followage", followErr: boom,
			want: []string{"Followage is unavailable right now."}},
		{name: "accountage lookup failure replies unavailable", text: "!accountage", ageErr: boom,
			want: []string{"Account age is unavailable right now."}},
		{name: "a disabled followage stays silent", text: "!followage", followage: following, modules: disabledModule("followage")},
		{name: "a disabled accountage stays silent", text: "!accountage", accountAge: oldAccount, modules: disabledModule("accountage")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := engine.Deps{
				Followage:  &fakeFollowage{result: tc.followage, err: tc.followErr},
				AccountAge: &fakeAccountAge{result: tc.accountAge, err: tc.ageErr},
				Proj:       &fakeProj{modules: tc.modules},
			}
			out := runChat(t, Followage(d), lookupContext(), tc.text)
			assert.Equal(t, tc.want, texts(out))
			for _, o := range out {
				assert.Equal(t, outgress.TypeChat, o.Type)
			}
		})
	}
}
