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

func liveStream(title, game string, viewers int) Stream {
	return Stream{
		UserFound: true, Live: true, Title: title, GameName: game,
		ViewerCount: viewers, StartedAt: channelClock.Add(-2 * time.Hour),
	}
}

func offlineStream(title, game string) Stream {
	return Stream{UserFound: true, Title: title, GameName: game}
}

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

func TestChannelRendersAnOfflineChannel(t *testing.T) {
	streams := &fakeStreams{values: map[string]Stream{"": offlineStream("bagel time", "Just Chatting")}}

	assert.Equal(t, "bagel time / Just Chatting / 0 / not right now",
		render(t, "{title} / {game} / {channel.viewers} / {uptime|not right now}", Chain{mounted(streams)}, nil))
}

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

func TestChannelFoldsItsOwnLoginOntoTheBareSpan(t *testing.T) {
	streams := &fakeStreams{values: map[string]Stream{"": liveStream("bagel time", "Just Chatting", 42)}}

	assert.Equal(t, "bagel time / bagel time",
		render(t, "{title:Streamer} / {title}", Chain{mounted(streams)}, nil))
	assert.Equal(t, []string{""}, streams.asked)
}

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

func TestChannelKeepsUnaddressableSpansLiteral(t *testing.T) {
	streams := &fakeStreams{values: map[string]Stream{"": liveStream("bagel time", "Just Chatting", 42)}}
	const template = "{title:} {channel.viewers:pokimane} {channel.followers}"

	assert.Equal(t, template, render(t, template, Chain{mounted(streams)}, nil))
	assert.Empty(t, streams.asked, "an unaddressable span costs no read")
}

type fakeCounts struct {
	result ChannelCountsResult
	calls  int
}

func (f *fakeCounts) Counts(context.Context) ChannelCountsResult {
	f.calls++
	return f.result
}

func TestChannelReadsFollowersAndSubsWithoutStreams(t *testing.T) {
	counts := &fakeCounts{result: ChannelCountsResult{Followers: 100, FollowersOK: true, Subs: 7, SubsOK: true}}
	chain := Chain{Channel{Counts: counts}}

	assert.Equal(t, "100 / 7", render(t, "{followers} / {subs}", chain, nil))
	assert.Equal(t, 1, counts.calls, "one read answers both spans")
}

func TestChannelLeavesANotOKCountLiteral(t *testing.T) {
	counts := &fakeCounts{result: ChannelCountsResult{Followers: 100, FollowersOK: true, SubsOK: false}}
	chain := Chain{Channel{Counts: counts}}

	assert.Equal(t, "100 {subs}", render(t, "{followers} {subs}", chain, nil))
	assert.Equal(t, "100 {subs|unknown}", render(t, "{followers} {subs|unknown}", chain, nil))
}

func TestChannelLeavesPayloadedCountSpansLiteral(t *testing.T) {
	counts := &fakeCounts{result: ChannelCountsResult{Followers: 100, FollowersOK: true}}
	chain := Chain{Channel{Counts: counts}}

	assert.Equal(t, "{followers:pokimane} 100", render(t, "{followers:pokimane} {followers}", chain, nil))
}

func TestChannelLeavesCountsLiteralWithoutTheDependency(t *testing.T) {
	assert.Equal(t, "{followers} {subs}", render(t, "{followers} {subs}", Chain{Channel{}}, nil))
}
