// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package livefeed

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/overlays/feed"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/ratelimit"
	"ItsBagelBot/pkg/valkey"

	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// Alerts queues one alert for the channel's overlay pages.
type Alerts interface {
	Publish(ctx context.Context, msg bus.OverlayMessage) error
}

// State sends a frame to the pages open right now; nothing is stored.
type State interface {
	Publish(ctx context.Context, subject string, payload []byte) error
}

// Plans reports whether a channel owner has a premium plan.
type Plans interface {
	Premium(ctx context.Context, userID uint64) (bool, error)
}

// Alphas reports whether a channel owner has the alpha account flag.
type Alphas interface {
	Alpha(ctx context.Context, userID uint64) (bool, error)
}

// Channels is the overlays repository as the feed sees it.
type Channels interface {
	FeedProfile(ctx context.Context, userID uint64) (repository.FeedProfile, error)
	AddFeedTotals(ctx context.Context, userID uint64, eventID string, delta feed.Delta) error
	FeedTotals(ctx context.Context, userID uint64) (repository.FeedTotals, error)
}

// Service folds channel events into overlay feed state and frames, and serves its snapshot and controls.
type Service struct {
	Client   valkey_go.Client
	Alerts   Alerts
	State    State
	Plans    Plans
	Alphas   Alphas
	Channels Channels
	Allow    func(context.Context, ratelimit.Request) (bool, error)
	Log      *zap.Logger
}

func (s *Service) store() store { return store{client: s.Client} }

func cached[T any](ctx context.Context, client valkey_go.Client, key string, load func() (T, error)) (T, error) {
	kv := valkey.NewKV(client)
	if value, found := valkey.GetJSON[T](ctx, kv, key); found {
		return value, nil
	}
	value, err := load()
	if err != nil {
		return value, err
	}
	return value, valkey.SetJSON(ctx, kv, valkey.Key{Name: key, TTL: profileTTL}, value)
}

func (s *Service) profile(ctx context.Context, userID string) (repository.FeedProfile, error) {
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return repository.FeedProfile{}, err
	}
	return cached(ctx, s.Client, profileKey(userID), func() (repository.FeedProfile, error) { return s.Channels.FeedProfile(ctx, id) })
}

func (s *Service) alpha(ctx context.Context, userID string) (bool, error) {
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return false, err
	}
	return s.Alphas.Alpha(ctx, id)
}

func (s *Service) requireAlpha(ctx context.Context, userID string) error {
	alpha, err := s.alpha(ctx, userID)
	if err != nil {
		return err
	}
	if !alpha {
		return overlaysrpc.ErrFeedAlphaRequired
	}
	return nil
}

// eligible is a channel with an overlay whose owner may use the new version.
func (s *Service) eligible(ctx context.Context, userID string) (repository.FeedProfile, bool, error) {
	profile, err := s.profile(ctx, userID)
	if err != nil || !profile.Owned {
		return profile, false, err
	}
	alpha, err := s.alpha(ctx, userID)
	return profile, alpha, err
}

func (s *Service) tier(ctx context.Context, c channel) (bus.OverlayTier, error) {
	id, err := strconv.ParseUint(string(c), 10, 64)
	if err != nil {
		return "", err
	}
	premium, err := cached(ctx, s.Client, tierKey(string(c)), func() (bool, error) { return s.Plans.Premium(ctx, id) })
	if premium {
		return bus.OverlayPremium, err
	}
	return bus.OverlayStandard, err
}

func (s *Service) totals(ctx context.Context, c channel) (repository.FeedTotals, error) {
	id, err := strconv.ParseUint(string(c), 10, 64)
	if err != nil {
		return repository.FeedTotals{}, err
	}
	return s.Channels.FeedTotals(ctx, id)
}

// alert is the wire body of one queued alert: the gateway reads event_id and expires_at, the page reads the event.
type alert struct {
	EventID   string    `json:"event_id"`
	ExpiresAt time.Time `json:"expires_at"`
	feed.Delivery
}

// publishAlert queues the delivery under id, the dedup key; a replay or test passes a fresh id.
func (s *Service) publishAlert(ctx context.Context, c channel, id string, delivery feed.Delivery) error {
	tier, err := s.tier(ctx, c)
	if err != nil {
		return err
	}
	value := alert{EventID: id, ExpiresAt: time.Now().Add(bus.OverlayClipTTL), Delivery: delivery}
	return s.Alerts.Publish(ctx, bus.OverlayMessage{Tier: tier, Kind: bus.OverlayKindCustom, UserID: string(c), EventID: id, Value: value})
}

func (s *Service) publishState(ctx context.Context, c channel, frame feed.Frame, data any) error {
	payload, err := codec.Marshal(overlaysrpc.StateFrame{Type: frame, Data: data})
	if err != nil {
		return err
	}
	return s.State.Publish(ctx, overlaysrpc.StateSubject(string(c)), payload)
}

func (s *Service) publishStats(ctx context.Context, c channel) error {
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
	return s.publishState(ctx, c, feed.FrameStats, stats)
}
