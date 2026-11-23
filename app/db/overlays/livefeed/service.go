// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package livefeed

import (
	"context"
	"strconv"

	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/overlays/feed"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/ratelimit"
	"ItsBagelBot/pkg/valkey"

	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// Frames publishes to OVERLAY_FEED and returns the stream sequence of the stored frame.
type Frames interface {
	Publish(ctx context.Context, subject, id string, payload []byte) (uint64, error)
}

// Channels is the overlays repository as the feed sees it.
type Channels interface {
	FeedProfile(ctx context.Context, userID uint64) (repository.FeedProfile, error)
	AddFeedTotals(ctx context.Context, userID uint64, delta feed.Delta) error
	FeedTotals(ctx context.Context, userID uint64) (repository.FeedTotals, error)
}

// Service folds channel events into overlay feed state and frames, and serves its snapshot and controls.
type Service struct {
	Client   valkey_go.Client
	Frames   Frames
	Channels Channels
	Allow    func(context.Context, ratelimit.Request) (bool, error)
	Log      *zap.Logger
}

func (s *Service) store() store { return store{client: s.Client} }

func (s *Service) profile(ctx context.Context, userID string) (repository.FeedProfile, error) {
	kv := valkey.NewKV(s.Client)
	if cached, found := valkey.GetJSON[repository.FeedProfile](ctx, kv, profileKey(userID)); found {
		return cached, nil
	}
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return repository.FeedProfile{}, err
	}
	profile, err := s.Channels.FeedProfile(ctx, id)
	if err != nil {
		return repository.FeedProfile{}, err
	}
	return profile, valkey.SetJSON(ctx, kv, valkey.Key{Name: profileKey(userID), TTL: profileTTL}, profile)
}

func (s *Service) totals(ctx context.Context, c channel) (repository.FeedTotals, error) {
	id, err := strconv.ParseUint(string(c), 10, 64)
	if err != nil {
		return repository.FeedTotals{}, err
	}
	return s.Channels.FeedTotals(ctx, id)
}

// publish stores one frame for the channel and moves its resume sequence forward.
func (s *Service) publish(ctx context.Context, c channel, frame feed.Frame, id string, value any) error {
	payload, err := codec.Marshal(value)
	if err != nil {
		return err
	}
	seq, err := s.Frames.Publish(ctx, feed.Subject(string(c), frame), id, payload)
	if err != nil {
		return err
	}
	return s.store().recordSeq(ctx, c, seq)
}

func (s *Service) publishStats(ctx context.Context, c channel, id string) error {
	session, err := s.store().session(ctx, c)
	if err != nil {
		return err
	}
	totals, err := s.totals(ctx, c)
	if err != nil {
		return err
	}
	stats, err := s.store().stats(ctx, c, session, totals)
	if err != nil {
		return err
	}
	return s.publish(ctx, c, feed.FrameStats, id+":stats", stats)
}
