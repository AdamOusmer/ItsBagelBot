// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/valkeytest"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type analyticsLive struct {
	liveAlways
	online bool
}

func (l analyticsLive) IsLive(context.Context, uint64) (bool, error) { return l.online, nil }

func TestCommandAnswersRequireAcceptedOutput(t *testing.T) {
	for _, tc := range []struct {
		name     string
		text     string
		perm     module.Role
		liveOnly bool
		fail     bool
		want     int
		engaged  bool
	}{
		{name: "unknown", text: "!missing"},
		{name: "silent", text: "!ping", engaged: true},
		{name: "permission", text: "!uptime", perm: module.RoleModerator},
		{name: "offline", text: "!uptime", liveOnly: true},
		{name: "publish failure", text: "!uptime", fail: true, engaged: true},
		{name: "batch", text: "!uptime", want: 2, engaged: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := cmdEmit("", module.KindCore, "uptime", "one\ntwo")
			m.Commands[0].Perm, m.Commands[0].LiveOnly = tc.perm, tc.liveOnly
			reg := NewRegistry(zap.NewNop(), m, cmdEmit("", module.KindCore, "ping", ""))
			pub := &fakePublisher{}
			if tc.fail {
				pub.failErr = errors.New("publish failed")
			}
			p := NewPipeline(Deps{Proj: fakeReader{}, Live: analyticsLive{}, Cooldown: NoopCooldown{}, Pub: pub, Log: zap.NewNop()}, reg, Config{})
			defer p.Close()
			c := &module.Context{BroadcasterID: 123, Env: chatEnv(tc.text, "")}
			state := &emitState{mctx: c, env: &c.Env}
			require.NoError(t, p.dispatchCommand(context.Background(), c, nil, p.newEmit(context.Background(), "123", state)))
			require.Equal(t, tc.want, c.Emitted)
			require.Equal(t, []bool{tc.want > 0, tc.engaged}, []bool{c.CommandAnswered, c.Engaged})
		})
	}
}

func TestTrialAnswersRequireAcceptedCommandOutput(t *testing.T) {
	for _, tc := range []struct {
		name, text, reply string
		perm              module.Role
		fail              bool
		answered          int64
	}{
		{name: "unknown", text: "!missing"},
		{name: "silent", text: "!ping"},
		{name: "permission", text: "!ping", reply: "pong", perm: module.RoleModerator},
		{name: "rejected", text: "!ping", reply: "pong", fail: true},
		{name: "accepted", text: "!ping", reply: "pong", answered: 1},
		{name: "multiline", text: "!ping", reply: "one\ntwo", answered: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := cmdEmit("", module.KindCore, "ping", tc.reply)
			m.Commands[0].Perm = tc.perm
			pub := &fakePublisher{}
			if tc.fail {
				pub.failErr = errors.New("publish failed")
			}
			bumper := &fakeBumper{}
			p := NewPipeline(Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: pub, Log: zap.NewNop(), Stats: bumper}, NewRegistry(zap.NewNop(), m), Config{})
			err := p.Process(trialLine{text: tc.text, gen: 1}.message(t))
			p.Close()
			require.Equal(t, tc.fail, err != nil)
			require.Equal(t, tc.answered, bumper.channelTotals()[123]["trial_answered"])
		})
	}
}

func TestDispatchRecordsGateRejections(t *testing.T) {
	for _, reason := range []string{"perm", "offline", "cooldown"} {
		t.Run(reason, func(t *testing.T) {
			vc := valkeytest.Spawn(t)
			m := cmdEmit("", module.KindCore, "ping", "pong")
			cmd := &m.Commands[0]
			cmd.Perm = module.RoleEveryone
			cmd.LiveOnly = reason == "offline"
			cmd.Cooldown = time.Second
			if reason == "perm" {
				cmd.Perm = module.RoleModerator
			}
			reg := NewRegistry(zap.NewNop(), m)
			rec := analytics.New(vc, zap.NewNop(), reg.AnalyticsNames(), analytics.WithoutTicker())
			p := NewPipeline(Deps{Proj: fakeReader{user: projection.User{}}, Live: analyticsLive{}, Cooldown: &denySecondCooldown{allowed: 1}, Pub: &fakePublisher{}, Log: zap.NewNop(), Analytics: rec}, reg, Config{})
			require.NoError(t, p.Process(commandMsg(t, "blocked", "!ping")))
			p.Close()
			rec.Close()
			counts, err := pkgvalkey.HashAllInts(context.Background(), vc, catalog.CounterKey(time.Now()))
			require.NoError(t, err)
			require.Equal(t, map[string]int64{"chat.messages": 1, "command.blocked|" + reason: 1}, counts)
		})
	}
}
