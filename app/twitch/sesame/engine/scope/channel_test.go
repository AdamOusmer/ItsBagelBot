// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var channelClock = time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)

type fakeStreams struct {
	values map[string]Stream
	asked  []string
}

func (f *fakeStreams) Stream(_ context.Context, login string) Stream {
	f.asked = append(f.asked, login)
	return f.values[login]
}

func live(s Stream) Stream {
	s.UserFound, s.Live, s.StartedAt = true, true, channelClock.Add(-2*time.Hour)
	return s
}

func mounted(streams Streams) Channel {
	return Channel{
		Locale: "en", Streams: streams, OwnLogin: "streamer",
		Uptime: true, Title: true, Game: true, Viewers: true,
		Now: func() time.Time { return channelClock },
	}
}

type fakeCounts struct {
	result ChannelCountsResult
	calls  int
}

func (f *fakeCounts) Counts(context.Context) ChannelCountsResult {
	f.calls++
	return f.result
}

func TestChannelMountsOnlyWhatIsWired(t *testing.T) {
	streams := &fakeStreams{values: map[string]Stream{"": live(Stream{Title: "bagel time", GameName: "Just Chatting", ViewerCount: 42})}}
	untitled := mounted(streams)
	untitled.Title = false
	tests := []struct {
		name     string
		channel  Channel
		template string
		want     string
	}{
		{
			name:     "leaves every token literal when nothing is wired",
			channel:  Channel{},
			template: "{uptime} {title} {game} {channel.viewers} {followers} {subs}",
			want:     "{uptime} {title} {game} {channel.viewers} {followers} {subs}",
		},
		{
			name:     "mounts each token on its own",
			channel:  untitled,
			template: "{title} / {game}",
			want:     "{title} / Just Chatting",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, render(t, tt.template, Chain{tt.channel}, nil))
		})
	}
}

func TestChannelStreamReads(t *testing.T) {
	bagel := live(Stream{Title: "bagel time", GameName: "Just Chatting", ViewerCount: 42})
	tests := []struct {
		name      string
		streams   map[string]Stream
		template  string
		want      string
		wantAsked []string
	}{
		{
			name:      "renders the live session with one read for four spans",
			streams:   map[string]Stream{"": bagel},
			template:  "{title} / {game} / {channel.viewers} / {uptime}",
			want:      "bagel time / Just Chatting / 42 / 2 hours",
			wantAsked: []string{""},
		},
		{
			name:      "renders an offline channel",
			streams:   map[string]Stream{"": {UserFound: true, Title: "bagel time", GameName: "Just Chatting"}},
			template:  "{title} / {game} / {channel.viewers} / {uptime|not right now}",
			want:      "bagel time / Just Chatting / 0 / not right now",
			wantAsked: []string{""},
		},
		{
			name:      "renders nothing for an unknown channel",
			streams:   map[string]Stream{},
			template:  "{title:ghost|-}/{game:ghost|-}",
			want:      "-/-",
			wantAsked: []string{"ghost"},
		},
		{
			name:      "reads a named channel and folds the at sign to the bare login",
			streams:   map[string]Stream{"": bagel, "pokimane": live(Stream{Title: "valorant", GameName: "VALORANT", ViewerCount: 30000})},
			template:  "{game:@Pokimane} / {game}",
			want:      "VALORANT / Just Chatting",
			wantAsked: []string{"pokimane", ""},
		},
		{
			name:      "folds its own login onto the bare span",
			streams:   map[string]Stream{"": bagel},
			template:  "{title:Streamer} / {title}",
			want:      "bagel time / bagel time",
			wantAsked: []string{""},
		},
		{
			name:     "keeps unaddressable spans literal without a read",
			streams:  map[string]Stream{"": bagel},
			template: "{title:} {channel.viewers:pokimane} {channel.followers}",
			want:     "{title:} {channel.viewers:pokimane} {channel.followers}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			streams := &fakeStreams{values: tt.streams}

			assert.Equal(t, tt.want, render(t, tt.template, Chain{mounted(streams)}, nil))
			assert.Equal(t, tt.wantAsked, streams.asked)
		})
	}
}

func TestChannelCapsNamedLogins(t *testing.T) {
	values := map[string]Stream{"": live(Stream{Title: "bagel time", GameName: "Just Chatting", ViewerCount: 42})}
	var template string
	for i := 0; i < MaxChannelLogins+1; i++ {
		login := "chan" + strconv.Itoa(i)
		values[login] = live(Stream{Title: "t" + strconv.Itoa(i), GameName: "g" + strconv.Itoa(i), ViewerCount: i})
		template += "{game:" + login + "|-}"
	}
	streams := &fakeStreams{values: values}

	assert.Equal(t, "g0g1g2-", render(t, template, Chain{mounted(streams)}, nil))
	assert.Len(t, streams.asked, MaxChannelLogins, "the fourth channel is never read")
}

func TestChannelCountSpans(t *testing.T) {
	tests := []struct {
		name     string
		result   ChannelCountsResult
		template string
		want     string
	}{
		{
			name:     "reads followers and subs without streams",
			result:   ChannelCountsResult{Followers: 100, FollowersOK: true, Subs: 7, SubsOK: true},
			template: "{followers} / {subs}",
			want:     "100 / 7",
		},
		{
			name:     "leaves a count that is not ok literal",
			result:   ChannelCountsResult{Followers: 100, FollowersOK: true},
			template: "{followers} {subs}",
			want:     "100 {subs}",
		},
		{
			name:     "leaves a not ok count literal even with a fallback",
			result:   ChannelCountsResult{Followers: 100, FollowersOK: true},
			template: "{followers} {subs|unknown}",
			want:     "100 {subs|unknown}",
		},
		{
			name:     "leaves payloaded count spans literal",
			result:   ChannelCountsResult{Followers: 100, FollowersOK: true},
			template: "{followers:pokimane} {followers}",
			want:     "{followers:pokimane} 100",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counts := &fakeCounts{result: tt.result}

			assert.Equal(t, tt.want, render(t, tt.template, Chain{Channel{Counts: counts}}, nil))
			assert.Equal(t, 1, counts.calls, "one read answers every count span")
		})
	}
}
