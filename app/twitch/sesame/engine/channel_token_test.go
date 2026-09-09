// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// stubStreamInfo answers the stream-info RPC from a fixture keyed by the
// address the engine used ("id/" for the command's own channel, "/login" for a
// named one) and records every call, so the round-trip claim is asserted.
type stubStreamInfo struct {
	byAddress map[string]StreamInfoResult
	err       error
	calls     []string
}

func (s *stubStreamInfo) Lookup(_ context.Context, broadcasterID, login string) (StreamInfoResult, error) {
	s.calls = append(s.calls, broadcasterID+"/"+login)
	return s.byAddress[broadcasterID+"/"+login], s.err
}

// channelFixture is one channel's wiring for the channel tokens: which module
// rows are set, and what the reader answers.
type channelFixture struct {
	response string
	modules  map[string]projection.ModuleView
	stream   StreamInfoLookup
}

func channelPipeline(t *testing.T, f channelFixture) *Pipeline {
	t.Helper()
	d := Deps{
		Proj: fakeReader{
			cmd:      projection.Command{Name: "brag", Response: f.response, IsActive: true, Perm: "everyone"},
			cmdFound: true,
			modules:  f.modules,
		},
		Live:       liveAlways{},
		Cooldown:   NoopCooldown{},
		Pub:        &fakePublisher{},
		StreamInfo: f.stream,
		Log:        zap.NewNop(),
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

// liveNow is the session the fixtures report: up for two hours, with an
// audience. The start is relative to the wall clock because {uptime} humanizes
// the distance from it, exactly as !uptime does.
func liveNow() StreamInfoResult {
	return StreamInfoResult{
		UserFound: true, Live: true, Title: "bagel time", GameName: "Just Chatting",
		ViewerCount: 42, StartedAt: time.Now().Add(-2 * time.Hour),
	}
}

// TestChannelTokensExpandThroughTheModuleGates is the table: each row is one
// template, one set of module rows, and the line chat sees.
func TestChannelTokensExpandThroughTheModuleGates(t *testing.T) {
	own := map[string]StreamInfoResult{"123/": liveNow()}

	cases := []struct {
		name    string
		fixture channelFixture
		want    string
	}{
		{
			name: "the whole family renders from one live session",
			fixture: channelFixture{
				response: "{title} / {game} / {channel.viewers} / {uptime}",
				stream:   &stubStreamInfo{byAddress: own},
			},
			want: "bagel time / Just Chatting / 42 / 2 hours",
		},
		{
			name: "a command's own toggle gates only its own token",
			fixture: channelFixture{
				response: "{title} / {game}",
				modules:  map[string]projection.ModuleView{TitleModuleName: off()},
				stream:   &stubStreamInfo{byAddress: own},
			},
			want: "{title} / Just Chatting",
		},
		{
			name: "the uptime toggle is the one !uptime reads",
			fixture: channelFixture{
				response: "up for {uptime}",
				modules:  map[string]projection.ModuleView{UptimeModuleName: off()},
				stream:   &stubStreamInfo{byAddress: own},
			},
			want: "up for {uptime}",
		},
		{
			name: "an offline channel is zero viewers and an empty uptime",
			fixture: channelFixture{
				response: "{channel.viewers} watching, live {uptime|nope}",
				stream: &stubStreamInfo{byAddress: map[string]StreamInfoResult{
					"123/": {UserFound: true, Title: "back soon", GameName: "Just Chatting"},
				}},
			},
			want: "0 watching, live nope",
		},
		{
			name: "a named channel resolves by login",
			fixture: channelFixture{
				response: "go watch {game:@Pokimane}",
				stream: &stubStreamInfo{byAddress: map[string]StreamInfoResult{
					"/pokimane": {UserFound: true, Live: true, GameName: "VALORANT"},
				}},
			},
			want: "go watch VALORANT",
		},
		{
			name: "a failed read renders the fallback, never the braces",
			fixture: channelFixture{
				response: "playing {game|something}",
				stream:   &stubStreamInfo{err: errors.New("boom")},
			},
			want: "playing something",
		},
		{
			name: "no reader wired leaves every span literal",
			fixture: channelFixture{
				response: "{title} {game} {uptime} {channel.viewers}",
			},
			want: "{title} {game} {uptime} {channel.viewers}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := channelPipeline(t, tc.fixture)
			assert.Equal(t, tc.want, expandViewer(t, p, "!brag"))
		})
	}
}

// One read serves the whole family, and a template naming no channel token
// reads nothing at all — the reason the scope is mounted off the lexed
// template rather than unconditionally.
func TestChannelTokensCostOneReadPerChannel(t *testing.T) {
	stream := &stubStreamInfo{byAddress: map[string]StreamInfoResult{
		"123/":      liveNow(),
		"/pokimane": {UserFound: true, Live: true, GameName: "VALORANT"},
	}}
	p := channelPipeline(t, channelFixture{
		response: "{title} {game} {uptime} {channel.viewers} {game:pokimane}",
		stream:   stream,
	})

	assert.Equal(t, "bagel time Just Chatting 2 hours 42 VALORANT", expandViewer(t, p, "!brag"))
	assert.Equal(t, []string{"123/", "/pokimane"}, stream.calls)

	quiet := &stubStreamInfo{}
	assert.Equal(t, "hi", expandViewer(t, channelPipeline(t, channelFixture{response: "hi", stream: quiet}), "!brag"))
	assert.Empty(t, quiet.calls, "a template naming no channel token never reads")
}
