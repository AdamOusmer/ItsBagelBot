// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/action"
	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/app/twitch/outgress/internal/conduit"
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/ratelimit"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

var (
	nodeRegion string
	nodeName   string
)

func SetNodeIdentity(region, host string) {
	nodeRegion = region
	nodeName = host
}

type Lane int

const (
	LanePremium Lane = iota
	LaneStandard
	LaneSystem
)

type expectedNackError string

func (e expectedNackError) Error() string      { return string(e) }
func (e expectedNackError) ExpectedNack() bool { return true }

const (
	ErrPaused          expectedNackError = "outgress is paused"
	errRateLimitFirst  expectedNackError = "rate limit exceeded on reserved bucket"
	errRateLimitShared expectedNackError = "rate limit exceeded on shared bucket"
)

type Worker struct {
	log      *zap.Logger
	limiter  ratelimit.Manager
	registry *channels.Registry
	twitch   *twitch.Client
	botID    string
	owner    string
	conduit  *conduit.Resolver
	lane     Lane
	batch    BatchStore
	actions  action.Registry
	userIDs  *cache.Cache[string]

	modVerifier *ModVerifier
	reauth      *ReauthNotifier
	factPub     bus.Publisher

	live       *LiveWriter
	streamInfo *projection.Store

	grants grantRegistry

	clipVerify chan struct{}
	tokenWarm  chan struct{}
}

type Config struct {
	Log      *zap.Logger
	Limiter  ratelimit.Manager
	Registry *channels.Registry
	Twitch   *twitch.Client
	BotID    string
	Owner    string
	Conduit  *conduit.Resolver
	Lane     Lane
	Batch    BatchStore
	UserIDs  *cache.Cache[string]
}

func New(cfg Config) *Worker {
	userIDs := cfg.UserIDs
	if userIDs == nil {
		userIDs = NewUserIDCache()
	}
	// A nil *channels.Registry in an interface would defeat the grant marker's nil guards.
	var grants grantRegistry
	if cfg.Registry != nil {
		grants = cfg.Registry
	}
	w := &Worker{
		grants:     grants,
		log:        cfg.Log,
		limiter:    cfg.Limiter,
		registry:   cfg.Registry,
		twitch:     cfg.Twitch,
		botID:      cfg.BotID,
		owner:      cfg.Owner,
		conduit:    cfg.Conduit,
		lane:       cfg.Lane,
		batch:      cfg.Batch,
		userIDs:    userIDs,
		clipVerify: make(chan struct{}, clipVerifySlots),
		tokenWarm:  make(chan struct{}, tokenWarmSlots),
	}
	w.actions = w.buildActions()
	return w
}

func (w *Worker) SetLiveWriter(lw *LiveWriter) { w.live = lw }

func (w *Worker) SetStreamInfoStore(s *projection.Store) { w.streamInfo = s }

func (w *Worker) SetModVerifier(v *ModVerifier) { w.modVerifier = v }

func (w *Worker) SetReauthNotifier(r *ReauthNotifier) { w.reauth = r }

func (w *Worker) SetFactPublisher(pub bus.Publisher) { w.factPub = pub }

const (
	UserIDCacheCapacity = 1024
	UserIDCacheTTL      = 10 * time.Minute
)

func NewUserIDCache() *cache.Cache[string] {
	return cache.New[string](UserIDCacheCapacity, UserIDCacheTTL)
}

func recordStageDuration(ctx context.Context, attribute string, started time.Time) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.AddAttribute(attribute, float64(time.Since(started).Microseconds())/1000)
	}
}

func noticeError(ctx context.Context, err error) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.NoticeError(err)
	}
}

func (w *Worker) botIdentity(action string, payload *outgress.Message) (string, bool) {
	id := payload.SenderID
	if id == "" {
		id = w.botID
	}
	if id == "" {
		w.log.Error("dropping "+action+": no bot identity configured",
			zap.String("broadcaster_id", payload.BroadcasterID))
		return "", false
	}
	return id, true
}

func (w *Worker) modStatus(_ context.Context, payload *outgress.Message, ch manage.Channel, found bool) bool {
	if w.modVerifier == nil {
		return found && ch.IsMod
	}
	return w.modVerifier.Status(ch, found, payload.BroadcasterID, payload.SenderID)
}

func (w *Worker) scheduleModStatus(broadcasterID, senderID string) {
	if w.modVerifier != nil {
		w.modVerifier.Schedule(broadcasterID, senderID)
	}
}
