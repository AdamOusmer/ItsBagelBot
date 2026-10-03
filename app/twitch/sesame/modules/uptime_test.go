// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type fakeUptime struct {
	result engine.UptimeResult
	err    error
	got    string
}

func (f *fakeUptime) Lookup(_ context.Context, broadcasterID string) (engine.UptimeResult, error) {
	f.got = broadcasterID
	return f.result, f.err
}

func TestUptimeReplies(t *testing.T) {
	live := engine.UptimeResult{Live: true, StartedAt: time.Now().Add(-2*time.Hour - 5*time.Minute)}
	cases := []struct {
		name    string
		service engine.UptimeLookup
		modules []projection.ModuleView
		want    []string
	}{
		{"reports the elapsed live time", &fakeUptime{result: live}, nil, []string{"The stream has been live for 2 hours, 5 minutes."}},
		{"reports offline when not live", &fakeUptime{}, nil, []string{"The stream is offline."}},
		{"reports unavailable when the lookup fails", &fakeUptime{err: errors.New("boom")}, nil, []string{"Stream uptime is unavailable right now."}},
		{"reports unavailable without a service", nil, nil, []string{"Stream uptime is unavailable right now."}},
		{"a disabled module stays silent", &fakeUptime{result: live}, disabledModule(uptimeModuleName), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := engine.Deps{Uptime: tc.service, Proj: &fakeProj{modules: tc.modules}, Log: zap.NewNop()}
			out := runChat(t, Uptime(d), lookupContext(), "!uptime")
			assert.Equal(t, tc.want, texts(out))
			for _, o := range out {
				assert.Equal(t, outgress.TypeChat, o.Type)
				assert.Equal(t, "5", o.BroadcasterID)
			}
		})
	}
}
