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

func TestStreamInfoReplies(t *testing.T) {
	liveReply := outgressrpc.StreamInfoReply{
		UserFound: true, Live: true, Title: "bagel time", GameName: "Just Chatting",
		ViewerCount: 42, StartedAt: streamStart,
	}
	offline := func(r *fakeStreamReader) {
		r.live["123"] = false
		r.channels = map[string]twitch.ChannelInfo{"123": {Title: "back soon", GameName: "Science & Technology"}}
	}
	tests := []struct {
		name  string
		setup func(*fakeStreamReader)
		req   outgressrpc.StreamInfoRequest
		want  outgressrpc.StreamInfoReply
	}{
		{"refuses a request addressing nobody", func(*fakeStreamReader) {}, outgressrpc.StreamInfoRequest{}, outgressrpc.StreamInfoReply{Error: "bad request"}},
		{"reads a live channel by id", func(*fakeStreamReader) {}, outgressrpc.StreamInfoRequest{BroadcasterID: "123"}, liveReply},
		{"resolves a login to a live channel", func(*fakeStreamReader) {}, outgressrpc.StreamInfoRequest{TargetLogin: "streamer"}, liveReply},
		{"reports an unknown login", func(*fakeStreamReader) {}, outgressrpc.StreamInfoRequest{TargetLogin: "ghost"}, outgressrpc.StreamInfoReply{UserFound: false}},
		{
			"falls back to the channel object when offline", offline, outgressrpc.StreamInfoRequest{BroadcasterID: "123"},
			outgressrpc.StreamInfoReply{UserFound: true, Title: "back soon", GameName: "Science & Technology"},
		},
		{
			"keeps the offline answer when the channel read fails",
			func(r *fakeStreamReader) { r.live["123"] = false; r.channelErr = errors.New("boom") },
			outgressrpc.StreamInfoRequest{BroadcasterID: "123"}, outgressrpc.StreamInfoReply{UserFound: true},
		},
		{
			"reports a failed stream read for a channel that exists",
			func(r *fakeStreamReader) { r.detailsErr = errors.New("boom") },
			outgressrpc.StreamInfoRequest{BroadcasterID: "123"}, outgressrpc.StreamInfoReply{UserFound: true, Error: "lookup failed"},
		},
		{
			"reports a failed login resolve", func(r *fakeStreamReader) { r.userErr = errors.New("boom") },
			outgressrpc.StreamInfoRequest{TargetLogin: "streamer"}, outgressrpc.StreamInfoReply{Error: "lookup failed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := liveReader()
			tt.setup(r)

			assert.Equal(t, tt.want, readInfo(r, tt.req))
		})
	}
}

func TestStreamInfoSpendsNoMoreHelixCallsThanItNeeds(t *testing.T) {
	tests := []struct {
		name      string
		req       outgressrpc.StreamInfoRequest
		wantCalls []string
	}{
		{"a request naming no channel costs no Helix call", outgressrpc.StreamInfoRequest{}, nil},
		{"a live channel addressed by id costs one call", outgressrpc.StreamInfoRequest{BroadcasterID: "123"}, []string{"streams:123"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := liveReader()

			readInfo(r, tt.req)

			assert.Equal(t, tt.wantCalls, r.calls)
		})
	}
}
