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

// channelClock is the fixed "now" {uptime} measures against, so a humanized
// span is an assertion rather than a race with the wall clock.
var channelClock = time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)

// fakeStreams answers a canned session per login ("" being the channel the
// command ran in) and records every login it was asked for, so the batching
// claim ("one read per channel per response") is asserted rather than assumed.
type fakeStreams struct {
	values map[string]Stream
	asked  []string
}

func (f *fakeStreams) Stream(_ context.Context, login string) Stream {
	f.asked = append(f.asked, login)
	return f.values[login]
}

// liveStream is a channel that has been up for two hours with an audience.
func liveStream(title, game string, viewers int) Stream {
	return Stream{
		UserFound: true, Live: true, Title: title, GameName: game,
		ViewerCount: viewers, StartedAt: channelClock.Add(-2 * time.Hour),
	}
}

// offlineStream is a channel Twitch knows but that is not broadcasting: it
// still has a title and a category (they come from the channel object, not the
// stream), and no audience.
func offlineStream(title, game string) Stream {
	return Stream{UserFound: true, Title: title, GameName: game}
}

// mounted is the scope with every token's gate on, which is what a broadcaster
// who has touched none of the toggles gets.
func mounted(streams Streams) Channel {
	return Channel{
		Locale: "en", Streams: streams, OwnLogin: "streamer",
		Uptime: true, Title: true, Game: true, Viewers: true,
		Now: func() time.Time { return channelClock },
	}
}

func TestChannelLeavesTokensLiteralWhenNothingIsWired(t *testing.T) {
	const template = "{uptime} {title} {game} {channel.viewers}"
	assert.Equal(t, template, render(t, template, Chain{Channel{}}, nil),
		"an unwired scope owns nothing, so its spans stay literal like a typo")
}

// Each of the three command-backed tokens is gated on its own row, so a
// channel that turned !title off keeps {game} working and shows {title} raw.
func TestChannelMountsEachTokenOnItsOwn(t *testing.T) {
	streams := &fakeStreams{values: map[string]Stream{"": liveStream("bagel time", "Just Chatting", 42)}}
	ch := mounted(streams)
	ch.Title = false

	assert.Equal(t, "{title} / Just Chatting", render(t, "{title} / {game}", Chain{ch}, nil))
}

func TestChannelRendersTheLiveSession(t *testing.T) {
	streams := &fakeStreams{values: map[string]Stream{"": liveStream("bagel time", "Just Chatting", 42)}}

	assert.Equal(t, "bagel time / Just Chatting / 42 / 2 hours",
		render(t, "{title} / {game} / {channel.viewers} / {uptime}", Chain{mounted(streams)}, nil))
	assert.Equal(t, []string{""}, streams.asked, "four spans, one read")
}

// The pinned offline decisions, together: the viewer count is "0" (a number
// the template can say out loud), the uptime is empty so its fallback speaks,
// and the title and category still render because an offline channel has both.
func TestChannelRendersAnOfflineChannel(t *testing.T) {
	streams := &fakeStreams{values: map[string]Stream{"": offlineStream("bagel time", "Just Chatting")}}

	assert.Equal(t, "bagel time / Just Chatting / 0 / not right now",
		render(t, "{title} / {game} / {channel.viewers} / {uptime|not right now}", Chain{mounted(streams)}, nil))
}

// A login Twitch does not know is a lookup that RAN and produced nothing:
// every span renders its fallback, and none of them stays literal.
func TestChannelRendersNothingForAnUnknownChannel(t *testing.T) {
	streams := &fakeStreams{values: map[string]Stream{}}

	assert.Equal(t, "-/-", render(t, "{title:ghost|-}/{game:ghost|-}", Chain{mounted(streams)}, nil))
}

func TestChannelReadsANamedChannel(t *testing.T) {
	streams := &fakeStreams{values: map[string]Stream{
		"":         liveStream("bagel time", "Just Chatting", 42),
		"pokimane": liveStream("valorant", "VALORANT", 30000),
	}}

	assert.Equal(t, "VALORANT / Just Chatting",
		render(t, "{game:@Pokimane} / {game}", Chain{mounted(streams)}, nil))
	assert.Equal(t, []string{"pokimane", ""}, streams.asked, "'@Pokimane' folds to the bare login")
}

// A span naming the broadcaster's own channel is the bare span: one read, and
// it does not spend one of the three named-login slots.
func TestChannelFoldsItsOwnLoginOntoTheBareSpan(t *testing.T) {
	streams := &fakeStreams{values: map[string]Stream{"": liveStream("bagel time", "Just Chatting", 42)}}

	assert.Equal(t, "bagel time / bagel time",
		render(t, "{title:Streamer} / {title}", Chain{mounted(streams)}, nil))
	assert.Equal(t, []string{""}, streams.asked)
}

// The cap is on distinct OTHER channels. Past it a span renders empty (its
// fallback speaks) rather than staying literal, and no further read is made.
func TestChannelCapsNamedLogins(t *testing.T) {
	values := map[string]Stream{"": liveStream("bagel time", "Just Chatting", 42)}
	var template string
	for i := 0; i < MaxChannelLogins+1; i++ {
		login := "chan" + strconv.Itoa(i)
		values[login] = liveStream("t"+strconv.Itoa(i), "g"+strconv.Itoa(i), i)
		template += "{game:" + login + "|-}"
	}
	streams := &fakeStreams{values: values}

	assert.Equal(t, "g0g1g2-", render(t, template+"", Chain{mounted(streams)}, nil))
	assert.Len(t, streams.asked, MaxChannelLogins, "the fourth channel is never read")
}

// The unknown-token rule, pinned beside the tokens that answer: an empty
// payload addresses nobody, and {channel.viewers} takes no payload at all.
func TestChannelKeepsUnaddressableSpansLiteral(t *testing.T) {
	streams := &fakeStreams{values: map[string]Stream{"": liveStream("bagel time", "Just Chatting", 42)}}
	const template = "{title:} {channel.viewers:pokimane} {channel.followers}"

	assert.Equal(t, template, render(t, template, Chain{mounted(streams)}, nil))
	assert.Empty(t, streams.asked, "an unaddressable span costs no read")
}
