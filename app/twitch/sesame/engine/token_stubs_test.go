// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/internal/projection"
)

type stubFollowage struct {
	result FollowageResult
	err    error
	calls  []string
}

func (s *stubFollowage) Lookup(_ context.Context, _, targetID, targetLogin string) (FollowageResult, error) {
	s.calls = append(s.calls, targetID+"/"+targetLogin)
	return s.result, s.err
}

type stubAccountAge struct {
	result AccountAgeResult
	calls  []string
}

func (s *stubAccountAge) Lookup(_ context.Context, targetID, targetLogin string) (AccountAgeResult, error) {
	s.calls = append(s.calls, targetID+"/"+targetLogin)
	return s.result, nil
}

type stubChannelCounts struct {
	result ChannelCountsResult
	calls  int
}

func (s *stubChannelCounts) Lookup(context.Context, string) (ChannelCountsResult, error) {
	s.calls++
	return s.result, nil
}

func on() projection.ModuleView { return projection.ModuleView{IsEnabled: true} }

func off() projection.ModuleView { return projection.ModuleView{IsEnabled: false} }

type stubStreamInfo struct {
	byAddress map[string]StreamInfoResult
	err       error
	calls     []string
}

func (s *stubStreamInfo) Lookup(_ context.Context, broadcasterID, login string) (StreamInfoResult, error) {
	s.calls = append(s.calls, broadcasterID+"/"+login)
	return s.byAddress[broadcasterID+"/"+login], s.err
}

func liveNow() StreamInfoResult {
	return StreamInfoResult{
		UserFound: true, Live: true, Title: "bagel time", GameName: "Just Chatting",
		ViewerCount: 42, StartedAt: time.Now().Add(-2 * time.Hour),
	}
}
