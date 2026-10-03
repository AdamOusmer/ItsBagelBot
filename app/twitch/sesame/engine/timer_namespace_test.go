// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"
)

func TestTimerModuleNamespaceUsesModuleGateAndOneRead(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			reads := 0
			feature := module.Module{Name: "facts", Kind: module.KindOptIn, Variables: []module.VariableGroup{{Name: "status", Fields: []string{"date"}, Read: func(_ context.Context, c *module.Context) (map[string]string, error) {
				reads++
				assert.Equal(t, uint64(42), c.BroadcasterID)
				assert.Empty(t, c.Env.ChatterUserID)
				return map[string]string{"date": "today"}, nil
			}}}}
			reader := fakeReader{modules: map[string]projection.ModuleView{"facts": {Name: "facts", IsEnabled: enabled}}}
			d := Deps{Proj: reader, Log: zap.NewNop()}
			p := NewPipeline(d, NewRegistry(d.Log, feature), Config{})
			t.Cleanup(p.Close)
			store, pub := timerFireStore(p)
			store.proj = reader
			text := "{facts:date}/{facts:status:date}/{if:facts:date:yes:no}/{user}"

			store.fire(context.Background(), armedTimer{ref: timerRef{broadcasterID: 42, id: "daily"}, def: timerDef{ID: "daily", Message: text}})

			if enabled {
				assert.Equal(t, []string{"today/today/yes/{user}"}, pub.chatTexts(t))
				assert.Equal(t, 1, reads)
				return
			}
			assert.Equal(t, []string{text}, pub.chatTexts(t))
			assert.Zero(t, reads)
		})
	}
}

func TestTimerNamespacesUseLoadedPremiumStatusAndConfiguredAccount(t *testing.T) {
	for _, tc := range []struct {
		name, account, status, want string
		active                      bool
	}{
		{name: "premium configured account", account: "Feinberg", status: "premium", active: true, want: "Feinberg/premium"},
		{name: "standard configured account", account: "Feinberg", status: "standard", active: true, want: "Feinberg/standard"},
		{name: "inactive premium account", account: "Feinberg", status: "premium", active: false, want: "Feinberg/standard"},
		{name: "timer has no implicit account", status: "premium", active: true, want: "unavailable/premium"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			feature := module.Module{Name: "facts", Kind: module.KindOptIn, Variables: []module.VariableGroup{{Name: "profile", Fields: []string{"player", "tier"}, Read: func(_ context.Context, c *module.Context) (map[string]string, error) {
				reads++
				assert.Empty(t, c.Env.ChatterUserID)
				assert.Empty(t, c.Env.BroadcasterUserLogin)
				assert.Equal(t, "fr", c.Locale)
				var config struct {
					Account string `json:"account"`
				}
				assert.NoError(t, c.Decode(&config))
				tier := "standard"
				if c.Regress.IsPremium() {
					tier = "premium"
				}
				return map[string]string{"player": config.Account, "tier": tier}, nil
			}}}}
			user := projection.User{Status: tc.status, IsActive: tc.active, Locale: "fr"}
			reader := fakeReader{user: user, modules: map[string]projection.ModuleView{"facts": {Name: "facts", IsEnabled: true, Configs: []byte(`{"account":"` + tc.account + `"}`)}}}
			d := Deps{Proj: reader, Log: zap.NewNop()}
			p := NewPipeline(d, NewRegistry(d.Log, feature), Config{})
			t.Cleanup(p.Close)
			store, pub := timerFireStore(p)
			store.proj = reader
			store.outgressPremium = premiumSubj
			store.fire(context.Background(), armedTimer{ref: timerRef{broadcasterID: 42, id: "daily"}, def: timerDef{ID: "daily", Message: "{facts:player|unavailable}/{facts:tier}"}})
			if assert.Len(t, pub.got, 1) {
				assert.Equal(t, tc.want, chatMessageText(t, pub.got[0].msg))
				wantSubject := standardSubj
				if user.Premium() {
					wantSubject = premiumSubj
				}
				assert.Equal(t, wantSubject, pub.got[0].subject)
			}
			assert.Equal(t, 1, reads)
		})
	}
}
