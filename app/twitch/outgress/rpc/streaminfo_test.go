// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// fakeStreamReader stands in for *twitch.Client's three Helix reads and
// records which of them the handler actually made, so "a live channel pays no
// second call" is asserted rather than assumed.
type fakeStreamReader struct {
	users    map[string]string
	details  map[string]twitch.StreamDetails
	live     map[string]bool
	channels map[string]twitch.ChannelInfo

	userErr    error
	detailsErr error
	channelErr error

	calls []string
}

func (f *fakeStreamReader) UserIDByLogin(_ context.Context, login string) (string, error) {
	f.calls = append(f.calls, "users:"+login)
	return f.users[login], f.userErr
}

func (f *fakeStreamReader) StreamDetails(_ context.Context, id string) (twitch.StreamDetails, bool, error) {
	f.calls = append(f.calls, "streams:"+id)
	return f.details[id], f.live[id], f.detailsErr
}

func (f *fakeStreamReader) ChannelInfo(_ context.Context, id string) (twitch.ChannelInfo, error) {
	f.calls = append(f.calls, "channels:"+id)
	return f.channels[id], f.channelErr
}

var streamStart = time.Date(2026, time.September, 9, 10, 0, 0, 0, time.UTC)

func liveReader() *fakeStreamReader {
	return &fakeStreamReader{
		users: map[string]string{"streamer": "123"},
		live:  map[string]bool{"123": true},
		details: map[string]twitch.StreamDetails{"123": {
			Title: "bagel time", GameName: "Just Chatting", ViewerCount: 42, StartedAt: streamStart,
		}},
	}
}

func readInfo(r streamReader, req outgressrpc.StreamInfoRequest) outgressrpc.StreamInfoReply {
	return readStreamInfo(context.Background(), r, zap.NewNop(), req)
}

func TestStreamInfoRefusesARequestAddressingNobody(t *testing.T) {
	r := liveReader()
	assert.Equal(t, "bad request", readInfo(r, outgressrpc.StreamInfoRequest{}).Error)
	assert.Empty(t, r.calls, "a request naming no channel costs no Helix call")
}

// A live channel is answered by Get Streams alone: title and category ride the
// same response, so nothing else is read.
func TestStreamInfoReadsALiveChannelInOneCall(t *testing.T) {
	r := liveReader()
	got := readInfo(r, outgressrpc.StreamInfoRequest{BroadcasterID: "123"})

	assert.Equal(t, outgressrpc.StreamInfoReply{
		UserFound: true, Live: true, Title: "bagel time", GameName: "Just Chatting",
		ViewerCount: 42, StartedAt: streamStart,
	}, got)
	assert.Equal(t, []string{"streams:123"}, r.calls)
}

// A login costs the Get Users hop first, and the reply carries the same
// snapshot: sesame has no Twitch credentials, so this side resolves the login.
func TestStreamInfoResolvesALogin(t *testing.T) {
	r := liveReader()
	got := readInfo(r, outgressrpc.StreamInfoRequest{TargetLogin: "streamer"})

	assert.True(t, got.Live)
	assert.Equal(t, "bagel time", got.Title)
	assert.Equal(t, []string{"users:streamer", "streams:123"}, r.calls)
}

// A login Twitch does not know is a resolved answer, not a failure: UserFound
// is false and Error stays empty, so the caller renders nothing rather than
// leaving the token literal.
func TestStreamInfoReportsAnUnknownLogin(t *testing.T) {
	r := liveReader()
	got := readInfo(r, outgressrpc.StreamInfoRequest{TargetLogin: "ghost"})

	assert.Equal(t, outgressrpc.StreamInfoReply{UserFound: false}, got)
	assert.Equal(t, []string{"users:ghost"}, r.calls)
}

// The offline path is the whole reason this handler is more than a
// StreamDetails passthrough: Get Streams answers nothing for a dark channel,
// so the title and category come from Get Channel Information, the very call
// !title makes.
func TestStreamInfoFallsBackToTheChannelObjectWhenOffline(t *testing.T) {
	r := liveReader()
	r.live["123"] = false
	r.channels = map[string]twitch.ChannelInfo{"123": {Title: "back soon", GameName: "Science & Technology"}}

	got := readInfo(r, outgressrpc.StreamInfoRequest{BroadcasterID: "123"})

	assert.Equal(t, outgressrpc.StreamInfoReply{
		UserFound: true, Title: "back soon", GameName: "Science & Technology",
	}, got)
	assert.Equal(t, []string{"streams:123", "channels:123"}, r.calls)
}

// An unreadable channel object degrades to the offline facts we do have. It is
// NOT an error: the stream read succeeded, and reporting the whole reply as
// failed would make a known-offline channel indistinguishable from an
// unreachable Twitch.
func TestStreamInfoKeepsTheOfflineAnswerWhenTheChannelReadFails(t *testing.T) {
	r := liveReader()
	r.live["123"] = false
	r.channelErr = errors.New("boom")

	assert.Equal(t, outgressrpc.StreamInfoReply{UserFound: true},
		readInfo(r, outgressrpc.StreamInfoRequest{BroadcasterID: "123"}))
}

func TestStreamInfoReportsAFailedStreamRead(t *testing.T) {
	r := liveReader()
	r.detailsErr = errors.New("boom")

	got := readInfo(r, outgressrpc.StreamInfoRequest{BroadcasterID: "123"})
	assert.Equal(t, "lookup failed", got.Error)
	assert.True(t, got.UserFound, "the channel exists; only the read did not")
}

func TestStreamInfoReportsAFailedLoginResolve(t *testing.T) {
	r := liveReader()
	r.userErr = errors.New("boom")

	assert.Equal(t, outgressrpc.StreamInfoReply{Error: "lookup failed"},
		readInfo(r, outgressrpc.StreamInfoRequest{TargetLogin: "streamer"}))
}
