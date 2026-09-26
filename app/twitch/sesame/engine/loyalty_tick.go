// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	loyaltyTickKeyPrefix       = "loyaltick:"
	loyaltyTickClaimPrefix     = "loyaltick:claim:"
	loyaltySchedulePrefix      = "loyaltick:state:"
	loyaltyDueKey              = "loyaltick:due"
	loyaltyDiscoveryCursorKey  = "loyaltick:discovery:cursor"
	loyaltyDiscoveryQueueKey   = "loyaltick:discovery:queue"
	watchTickInterval          = 5 * time.Minute
	watchTickJitter            = time.Minute
	loyaltyTickClaimTTL        = 30 * time.Second
	chattersRPCTimeout         = 12 * time.Second
	loyaltyPageTimeout         = 20 * time.Second
	loyaltyRearmTimeout        = 5 * time.Second
	watchTickQuickRetry        = time.Minute
	watchTickQuickRetries      = 2
	watchTickReconfirmInterval = time.Hour
	watchCollectionMaxAge      = 10 * time.Minute
	loyaltyEscalationLevel     = 3
	loyaltyReconcileInterval   = time.Minute
	loyaltyReconcileClaimKey   = loyaltyTickClaimPrefix + "reconcile"
	loyaltyReconcileClaimTTL   = 30 * time.Second
	loyaltyPollInterval        = time.Second
	loyaltyWorkers             = 4
	loyaltyQueueSize           = 64
	loyaltyStateRetention      = 30 * 24 * time.Hour
)

// The sorted set is the durable source of scheduled work. Expiry notifications
// only wake the bounded poller; a restart or dropped subscription cannot erase
// an outstanding window. Each worker handles ONE page, then yields the channel
// to the shared queue so large channels cannot occupy a worker for every page.
type ValkeyLoyaltyClock struct {
	client          valkey.Client
	proj            projection.Reader
	live            IsLiveChecker
	reporter        *LoyaltyReporter // retained constructor compatibility; watch awards use the outbox
	awards          *watchtime.Store
	viewers         *ValkeyChatters
	nc              *nats.Conn
	request         func(context.Context, string, []byte) (*nats.Msg, error)
	chattersSubject string
	rearmSubject    string
	botID           string
	keyspaceDB      int
	log             *zap.Logger
	wake            chan struct{}
	rearms          chan uint64
	now             func() time.Time
}

type LoyaltyClockConfig struct {
	OutgressRPCPrefix        string
	ModulesInvalidateSubject string
	BotUserID                string
	KeyspaceDB               int
	// Retained for rolling wiring compatibility. Reconfirmation now occurs in
	// the correlated chatter request and requires a successful Twitch result.
	Publisher             bus.Publisher
	OutgressSystemSubject string
	Log                   *zap.Logger
	ViewerSnapshots       *ValkeyChatters
}

func NewValkeyLoyaltyClock(client valkey.Client, nc *nats.Conn, proj projection.Reader, live IsLiveChecker, reporter *LoyaltyReporter, cfg LoyaltyClockConfig) *ValkeyLoyaltyClock {
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	if cfg.OutgressRPCPrefix == "" {
		cfg.OutgressRPCPrefix = "bagel.rpc.outgress"
	}
	primary := pkg_valkey.Primary(client)
	return &ValkeyLoyaltyClock{client: primary, proj: proj, live: live, reporter: reporter, awards: watchtime.NewStore(primary), viewers: cfg.ViewerSnapshots, nc: nc,
		request: func(ctx context.Context, subject string, body []byte) (*nats.Msg, error) {
			return bus.RequestWithContext(ctx, nc, subject, body)
		},
		chattersSubject: cfg.OutgressRPCPrefix + ".chatters.get", rearmSubject: cfg.ModulesInvalidateSubject, botID: cfg.BotUserID, keyspaceDB: cfg.KeyspaceDB,
		log: log, wake: make(chan struct{}, 1), rearms: make(chan uint64, loyaltyQueueSize), now: time.Now}
}

func loyaltyTickKey(id uint64) string     { return cache.UserKey(loyaltyTickKeyPrefix, id) }
func loyaltyScheduleKey(id uint64) string { return cache.UserKey(loyaltySchedulePrefix, id) }
func loyaltyClaimKey(id uint64) string    { return cache.UserKey(loyaltyTickClaimPrefix, id) }

// Arm is the recovery/legacy surface. An already active schedule stays intact;
// only a real versioned online event can start a new stream's window.
func (s *ValkeyLoyaltyClock) Arm(ctx context.Context, id uint64) { s.arm(ctx, id, 0, false) }
func (s *ValkeyLoyaltyClock) ArmVersioned(ctx context.Context, id uint64, version int64) {
	s.arm(ctx, id, version, true)
}
func (s *ValkeyLoyaltyClock) arm(ctx context.Context, id uint64, version int64, online bool) bool {
	if id == 0 {
		return true
	}
	snap, allowed, err := s.awards.Capture(ctx, id)
	if err != nil {
		s.log.Warn("loyalty: admission unavailable while arming", module.BIDField(id), zap.Error(err))
		return false
	}
	if !allowed || snap.LiveSession == "" {
		return true
	}
	if version == 0 {
		raw, err := s.client.Do(ctx, s.client.B().Get().Key(livekey.Key(id)).Build()).ToString()
		if err != nil {
			return false
		}
		version, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return true
		}
	}
	offset := time.Duration(rand.Int64N(int64(watchTickJitter/time.Second)+1)) * time.Second
	due := s.now().Add(watchTickInterval + offset).UnixMilli()
	mode := "recover"
	if online {
		mode = "online"
	}
	_, err = s.eval(ctx, loyaltyArmScript, []string{loyaltyScheduleKey(id), loyaltyDueKey, loyaltyClaimKey(id)},
		strconv.FormatUint(id, 10), strconv.FormatInt(version, 10), snap.Generation, strconv.FormatInt(version, 10), strconv.FormatInt(due, 10), mode)
	if err != nil {
		s.log.Warn("loyalty: failed to persist watch schedule", module.BIDField(id), zap.Error(err))
		return false
	}
	s.nudge()
	return true
}

func (s *ValkeyLoyaltyClock) Disarm(ctx context.Context, id uint64) {
	s.DisarmVersioned(ctx, id, livekey.VersionNow())
}
func (s *ValkeyLoyaltyClock) DisarmVersioned(ctx context.Context, id uint64, version int64) {
	if id == 0 {
		return
	}
	if version == 0 {
		version = livekey.VersionNow()
	}
	_, err := s.eval(ctx, loyaltyDisarmScript, []string{loyaltyScheduleKey(id), loyaltyDueKey, loyaltyTickKey(id), loyaltyClaimKey(id)},
		strconv.FormatUint(id, 10), strconv.FormatInt(version, 10), strconv.FormatInt(loyaltyStateRetention.Milliseconds(), 10))
	if err != nil {
		s.log.Warn("loyalty: failed to stop watch schedule", module.BIDField(id), zap.Error(err))
	}
}

func (s *ValkeyLoyaltyClock) nudge() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *ValkeyLoyaltyClock) onExpired(_ context.Context, key string) {
	if strings.HasPrefix(key, loyaltyTickKeyPrefix) && !strings.HasPrefix(key, loyaltyTickClaimPrefix) {
		s.nudge()
	}
}
func (s *ValkeyLoyaltyClock) StartExpiryWatcher(ctx context.Context) {
	channel := "__keyevent@" + strconv.Itoa(s.keyspaceDB) + "__:expired"
	for ctx.Err() == nil {
		err := s.client.Receive(ctx, s.client.B().Subscribe().Channel(channel).Build(), func(msg valkey.PubSubMessage) { s.onExpired(ctx, msg.Message) })
		if ctx.Err() != nil {
			return
		}
		s.log.Warn("loyalty: expiry wake watcher disconnected", zap.Error(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
func (s *ValkeyLoyaltyClock) StartRearmWatcher(ctx context.Context) {
	if s.nc == nil || s.rearmSubject == "" {
		return
	}
	sub, err := s.nc.Subscribe(s.rearmSubject, func(msg *nats.Msg) {
		var dto invalidate.DTO
		if codec.Unmarshal(msg.Data, &dto) != nil {
			return
		}
		if id, err := strconv.ParseUint(dto.BroadcasterID, 10, 64); err == nil && id != 0 {
			select {
			case s.rearms <- id:
			default:
				s.nudge()
			}
		}
	})
	if err != nil {
		s.log.Error("loyalty: rearm watcher unavailable", zap.Error(err))
		return
	}
	defer sub.Unsubscribe()
	<-ctx.Done()
}

// StartReconciler runs the persisted due queue and periodically discovers live
// channels. The per-page contexts bound requests and writes below the lease;
// ownership checks also fence a paused worker after its lease has expired.
func (s *ValkeyLoyaltyClock) StartReconciler(ctx context.Context) {
	jobs := make(chan uint64, loyaltyQueueSize)
	for range loyaltyWorkers {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case id := <-jobs:
					s.fire(ctx, id)
				case id := <-s.rearms:
					rearmCtx, rearmCancel := context.WithTimeout(ctx, loyaltyRearmTimeout)
					s.Arm(rearmCtx, id)
					rearmCancel()
				}
			}
		}()
	}
	poll := time.NewTicker(loyaltyPollInterval)
	defer poll.Stop()
	reconcile := time.NewTicker(loyaltyReconcileInterval)
	defer reconcile.Stop()
	s.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-reconcile.C:
			s.reconcile(ctx)
		case <-poll.C:
			s.queueDue(ctx, jobs)
		case <-s.wake:
			s.queueDue(ctx, jobs)
		}
	}
}
func (s *ValkeyLoyaltyClock) queueDue(ctx context.Context, jobs chan<- uint64) {
	pctx, cancel := context.WithTimeout(ctx, loyaltyRearmTimeout)
	defer cancel()
	result, err := s.eval(pctx, loyaltyDueScript, []string{loyaltyDueKey}, strconv.FormatInt(s.now().UnixMilli(), 10), strconv.Itoa(loyaltyQueueSize))
	if err != nil {
		s.log.Warn("loyalty: due queue read failed", zap.Error(err))
		return
	}
	ids, err := result.AsStrSlice()
	if err != nil {
		return
	}
	for _, raw := range ids {
		if id, err := strconv.ParseUint(raw, 10, 64); err == nil && id != 0 {
			select {
			case jobs <- id:
			default:
				return
			}
		}
	}
}
func (s *ValkeyLoyaltyClock) reconcile(ctx context.Context) {
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if won, err := pkg_valkey.ClaimOnce(rctx, s.client, loyaltyReconcileClaimKey, loyaltyReconcileClaimTTL); err != nil || !won {
		return
	}
	// Persist both the SCAN cursor and its pending page. A large keyspace or
	// slow admission reads must not restart discovery from zero every minute.
	completedScan := false
	for rctx.Err() == nil {
		raw, err := s.client.Do(rctx, s.client.B().Lindex().Key(loyaltyDiscoveryQueueKey).Index(0).Build()).ToString()
		if err == nil {
			id, parseErr := strconv.ParseUint(raw, 10, 64)
			if parseErr == nil && !s.arm(rctx, id, 0, false) {
				return
			}
			_, err = s.eval(rctx, loyaltyDiscoveryPopScript, []string{loyaltyDiscoveryQueueKey}, raw)
			if err != nil {
				return
			}
			continue
		}
		if !valkey.IsValkeyNil(err) {
			return
		}
		if completedScan {
			return
		}
		rawCursor, err := s.client.Do(rctx, s.client.B().Get().Key(loyaltyDiscoveryCursorKey).Build()).ToString()
		if err != nil && !valkey.IsValkeyNil(err) {
			return
		}
		cursor, _ := strconv.ParseUint(rawCursor, 10, 64)
		scan, err := s.client.Do(rctx, s.client.B().Scan().Cursor(cursor).Match(livekey.KeyPrefix+"*").Count(200).Build()).AsScanEntry()
		if err != nil {
			s.log.Warn("loyalty: live schedule discovery failed", zap.Error(err))
			return
		}
		args := []string{strconv.FormatUint(scan.Cursor, 10)}
		for _, key := range scan.Elements {
			if id, ok := parseLiveKey(key); ok {
				args = append(args, strconv.FormatUint(id, 10))
			}
		}
		_, err = s.eval(rctx, loyaltyDiscoverySaveScript, []string{loyaltyDiscoveryCursorKey, loyaltyDiscoveryQueueKey}, args...)
		if err != nil {
			return
		}
		completedScan = scan.Cursor == 0
	}
}
func parseLiveKey(key string) (uint64, bool) {
	if !strings.HasPrefix(key, livekey.KeyPrefix) || strings.HasPrefix(key, recheckKeyPrefix) {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(key, livekey.KeyPrefix), 10, 64)
	return id, err == nil && id != 0
}

func (s *ValkeyLoyaltyClock) eval(ctx context.Context, script string, keys []string, args ...string) (valkey.ValkeyResult, error) {
	result := s.client.Do(ctx, s.client.B().Eval().Script(script).Numkeys(int64(len(keys))).Key(keys...).Arg(args...).Build())
	return result, result.Error()
}

// fire owns at most one page. Cursor progress advances only after the exact
// saved payload reaches the durable outbox; a crash between those operations
// replays the immutable chunk rather than refetching a changing chatter page.
func (s *ValkeyLoyaltyClock) fire(ctx context.Context, id uint64) {
	ctx, cancel := context.WithTimeout(ctx, loyaltyPageTimeout)
	defer cancel()
	owner := nuid.Next()
	result, err := s.eval(ctx, loyaltyClaimScript, []string{loyaltyScheduleKey(id), loyaltyDueKey, loyaltyClaimKey(id)},
		strconv.FormatUint(id, 10), owner, strconv.FormatInt(s.now().UnixMilli(), 10), strconv.FormatInt(loyaltyTickClaimTTL.Milliseconds(), 10))
	if err != nil {
		s.log.Warn("loyalty: window claim failed", module.BIDField(id), zap.Error(err))
		return
	}
	won, err := result.AsInt64()
	if err != nil || won != 1 {
		return
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), loyaltyRearmTimeout)
		defer releaseCancel()
		_ = pkg_valkey.NewOwnerLock(s.client, loyaltyClaimKey(id), owner).Release(releaseCtx)
	}()
	state, err := s.readSchedule(ctx, id)
	if err != nil {
		s.retry(ctx, id, owner, err, 0)
		return
	}
	pageStarted := time.Now()
	defer func() {
		age := max(int64(0), s.now().UnixMilli()-state.startedAt)
		dueAge := max(int64(0), s.now().UnixMilli()-state.dueAt)
		s.log.Debug("loyalty: watch page processed", module.BIDField(id),
			zap.String("window_id", state.window), zap.Uint32("chunk", state.chunk),
			zap.Int64("collection_age_ms", age), zap.Int64("due_age_ms", dueAge),
			zap.Int64("overdue_intervals", dueAge/watchTickInterval.Milliseconds()),
			zap.Duration("processing_duration", time.Since(pageStarted)))
	}()
	snap, allowed, err := s.awards.Capture(ctx, id)
	if err != nil {
		s.retry(ctx, id, owner, err, 0)
		return
	}
	if !allowed || snap.LiveSession == "" || snap.Generation != state.generation || snap.LiveSession != state.liveSession {
		s.stopOwned(ctx, id, owner, "admission changed")
		return
	}
	checkLive := state.confirmedAt == 0 || s.now().UnixMilli()-state.confirmedAt >= watchTickReconfirmInterval.Milliseconds()
	if state.pending != "" && !checkLive {
		s.submitPending(ctx, id, owner, state)
		return
	}
	if state.pending == "" && s.collectionExpired(state) {
		s.abandonWindow(ctx, id, owner, "collection age exceeded")
		return
	}
	var cfg LoyaltyModuleConfig
	if len(snap.Config) > 0 {
		if err := codec.Unmarshal(snap.Config, &cfg); err != nil {
			s.retry(ctx, id, owner, fmt.Errorf("invalid loyalty configuration: %w", err), 0)
			return
		}
	}
	reply, err := s.fetchPage(ctx, id, state, checkLive)
	if err != nil {
		s.retry(ctx, id, owner, err, 0)
		return
	}
	if reply.CheckedAtUnixMilli > 0 {
		if !reply.Live {
			s.stopOwned(ctx, id, owner, "offline")
			return
		}
		changed, err := s.confirmStream(ctx, id, owner, reply)
		if err != nil || changed {
			return
		}
	}
	if state.pending != "" && reply.CheckedAtUnixMilli > 0 {
		// A saved snapshot may outlive the hourly live confirmation during an
		// outage. Reconfirm before accepting it, but never replace its immutable
		// viewers with the fresh page returned by the confirmation request.
		// A later page failure cannot invalidate a successful live check.
		s.submitPending(ctx, id, owner, state)
		return
	}
	if reply.Error != "" || reply.MissingScope {
		if reply.ErrorCode == "inactive" {
			s.stopOwned(ctx, id, owner, "tenant inactive")
			return
		}
		if reply.ErrorCode == "invalid" || reply.ErrorCode == "repeated_cursor" {
			s.abandonWindow(ctx, id, owner, reply.Error)
			return
		}
		s.retry(ctx, id, owner, &chattersError{message: reply.Error, missingScope: reply.MissingScope}, reply.RetryAtUnixMilli)
		return
	}
	if checkLive && reply.CheckedAtUnixMilli == 0 {
		s.retry(ctx, id, owner, errors.New("chatter reply omitted live confirmation"), 0)
		return
	}
	if !reply.Complete && (reply.NextCursor == "" || reply.NextCursor == state.cursor) {
		s.abandonWindow(ctx, id, owner, "chatter pagination did not advance")
		return
	}
	// The viewer cache represents a complete listing. Never publish a partial
	// watch page as the authoritative snapshot; larger listings retain the
	// existing on-demand viewer RPC refresh.
	if state.chunk == 0 && state.cursor == "" && reply.Complete {
		s.viewers.Store(ctx, id, viewerSnapshotEntries(reply.Chatters))
	}
	entries := make([]data.LoyaltyEarnEntry, 0, len(reply.Chatters))
	seen := map[uint64]struct{}{}
	for _, ch := range reply.Chatters {
		viewer, ok := s.chatterViewerID(ch.ID)
		if !ok {
			continue
		}
		if _, ok := seen[viewer]; ok {
			continue
		}
		seen[viewer] = struct{}{}
		entries = append(entries, data.LoyaltyEarnEntry{ViewerID: viewer, ViewerLogin: ch.Login, Points: cfg.EffectiveWatchPointsPerTick(), WatchSeconds: uint64(watchTickInterval / time.Second)})
	}
	dto := data.WatchAwardDTO{UserID: id, Generation: state.generation, LiveSession: state.liveSession, WindowID: state.window, WindowStartedAtUnixMilli: state.startedAt, Chunk: state.chunk, AccountCreatedAt: snap.AccountCreatedAt, Entries: entries}
	payload, err := codec.Marshal(dto)
	if err != nil {
		s.retry(ctx, id, owner, err, 0)
		return
	}
	complete := "0"
	if reply.Complete {
		complete = "1"
	}
	saved, err := s.eval(ctx, loyaltySavePageScript, []string{loyaltyScheduleKey(id), loyaltyClaimKey(id)}, owner, state.window, string(payload), reply.NextCursor, complete)
	if err != nil {
		return
	}
	ok, err := saved.AsInt64()
	if err != nil || ok != 1 {
		if err == nil && ok == -1 {
			s.abandonWindow(ctx, id, owner, "chatter pagination cursor cycle")
		}
		return
	}
	state.pending = string(payload)
	state.nextCursor = reply.NextCursor
	state.complete = reply.Complete
	s.submitPending(ctx, id, owner, state)
}

type loyaltySchedule struct {
	generation, liveSession, window, cursor, pending, nextCursor string
	chunk                                                        uint32
	confirmedAt                                                  int64
	startedAt                                                    int64
	dueAt                                                        int64
	complete                                                     bool
}

func (s *ValkeyLoyaltyClock) readSchedule(ctx context.Context, id uint64) (loyaltySchedule, error) {
	fields, err := s.client.Do(ctx, s.client.B().Hgetall().Key(loyaltyScheduleKey(id)).Build()).AsStrMap()
	if err != nil {
		return loyaltySchedule{}, err
	}
	chunk, err := strconv.ParseUint(fields["chunk"], 10, 32)
	if err != nil {
		return loyaltySchedule{}, err
	}
	confirmed, _ := strconv.ParseInt(fields["confirmed_at"], 10, 64)
	started, _ := strconv.ParseInt(fields["window_started_at"], 10, 64)
	due, _ := strconv.ParseInt(fields["due"], 10, 64)
	return loyaltySchedule{generation: fields["generation"], liveSession: fields["live_session"], window: fields["window"], cursor: fields["cursor"], pending: fields["pending"], nextCursor: fields["pending_cursor"], chunk: uint32(chunk), confirmedAt: confirmed, startedAt: started, dueAt: due, complete: fields["pending_complete"] == "1"}, nil
}

func (s *ValkeyLoyaltyClock) collectionExpired(state loyaltySchedule) bool {
	return state.startedAt == 0 || s.now().UnixMilli()-state.startedAt >= watchCollectionMaxAge.Milliseconds()
}
func (s *ValkeyLoyaltyClock) submitPending(ctx context.Context, id uint64, owner string, state loyaltySchedule) {
	var dto data.WatchAwardDTO
	if err := codec.Unmarshal([]byte(state.pending), &dto); err != nil {
		s.abandonWindow(ctx, id, owner, "invalid saved award: "+err.Error())
		return
	}
	if len(dto.Entries) > 0 {
		if err := watchtime.ValidateAward(dto); err != nil {
			s.abandonWindow(ctx, id, owner, "invalid saved award: "+err.Error())
			return
		}
		accepted, err := s.awards.EnqueueOwned(ctx, dto, owner)
		if err != nil {
			s.retry(ctx, id, owner, err, 0)
			return
		}
		if !accepted {
			s.stopOwned(ctx, id, owner, "award admission revoked")
			return
		}
	}
	if !state.complete && s.collectionExpired(state) {
		s.abandonWindow(ctx, id, owner, "collection age exceeded after saved page")
		return
	}
	complete := "0"
	if state.complete {
		complete = "1"
	}
	_, err := s.eval(ctx, loyaltyCommitPageScript, []string{loyaltyScheduleKey(id), loyaltyDueKey, loyaltyClaimKey(id)},
		owner, strconv.FormatUint(id, 10), state.window, state.pending, complete, strconv.FormatInt(s.now().UnixMilli(), 10), strconv.FormatInt(watchTickInterval.Milliseconds(), 10))
	if err != nil {
		s.log.Warn("loyalty: cursor commit failed; saved chunk will resume", module.BIDField(id), zap.Error(err))
	}
}
func (s *ValkeyLoyaltyClock) fetchPage(ctx context.Context, id uint64, state loyaltySchedule, checkLive bool) (manage.ChattersReply, error) {
	ctx, cancel := context.WithTimeout(ctx, chattersRPCTimeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	requestID := nuid.Next()
	req := manage.ChattersRequest{BroadcasterID: strconv.FormatUint(id, 10), RequestID: requestID, WindowID: state.window, SessionGeneration: state.generation, LiveSession: state.liveSession, Cursor: state.cursor, CheckLive: checkLive, DeadlineUnixMilli: deadline.UnixMilli()}
	body, err := codec.Marshal(req)
	if err != nil {
		return manage.ChattersReply{}, err
	}
	msg, err := s.request(ctx, s.chattersSubject, body)
	if err != nil {
		return manage.ChattersReply{}, err
	}
	var reply manage.ChattersReply
	if err := codec.Unmarshal(msg.Data, &reply); err != nil {
		return reply, err
	}
	if reply.BroadcasterID != req.BroadcasterID || reply.RequestID != requestID || reply.WindowID != state.window || reply.SessionGeneration != state.generation || reply.LiveSession != state.liveSession {
		return reply, errors.New("chatter reply correlation mismatch")
	}
	if len(reply.Chatters) > 1000 {
		return reply, errors.New("chatter page exceeds limit")
	}
	if reply.CheckedAtUnixMilli > s.now().Add(time.Minute).UnixMilli() {
		return reply, errors.New("chatter live confirmation timestamp is in the future")
	}
	if reply.CheckedAtUnixMilli > 0 && reply.CheckedAtUnixMilli < s.now().Add(-time.Minute).UnixMilli() {
		return reply, errors.New("chatter live confirmation timestamp is stale")
	}
	if reply.CheckedAtUnixMilli > 0 && reply.Live && (reply.StreamID == "" || len(reply.StreamID) > 128 || reply.StreamStartedAtUnixMilli <= 0 || reply.StreamStartedAtUnixMilli > reply.CheckedAtUnixMilli) {
		return reply, errors.New("chatter live confirmation omitted a valid stream identity")
	}
	return reply, nil
}
func (s *ValkeyLoyaltyClock) confirmStream(ctx context.Context, id uint64, owner string, reply manage.ChattersReply) (bool, error) {
	result, err := s.eval(ctx, loyaltyConfirmScript, []string{loyaltyScheduleKey(id), loyaltyClaimKey(id), loyaltyDueKey}, owner,
		strconv.FormatInt(reply.CheckedAtUnixMilli, 10), reply.StreamID, strconv.FormatInt(reply.StreamStartedAtUnixMilli, 10),
		strconv.FormatUint(id, 10), strconv.FormatInt(s.now().Add(watchTickInterval).UnixMilli(), 10))
	if err != nil {
		return false, err
	}
	n, err := result.AsInt64()
	return n != 1, err
}
func (s *ValkeyLoyaltyClock) stopOwned(ctx context.Context, id uint64, owner, reason string) {
	_, err := s.eval(ctx, loyaltyStopScript, []string{loyaltyScheduleKey(id), loyaltyDueKey, loyaltyClaimKey(id)}, owner, strconv.FormatUint(id, 10), reason, strconv.FormatInt(loyaltyStateRetention.Milliseconds(), 10))
	if err != nil {
		s.log.Warn("loyalty: failed to revoke window", module.BIDField(id), zap.Error(err))
	}
}
func (s *ValkeyLoyaltyClock) abandonWindow(ctx context.Context, id uint64, owner, reason string) {
	_, err := s.eval(ctx, loyaltyAbandonScript, []string{loyaltyScheduleKey(id), loyaltyDueKey, loyaltyClaimKey(id)}, owner, strconv.FormatUint(id, 10), strconv.FormatInt(s.now().Add(watchTickInterval).UnixMilli(), 10), reason)
	if err != nil {
		s.log.Warn("loyalty: failed to abandon invalid window", module.BIDField(id), zap.Error(err))
	}
}
func (s *ValkeyLoyaltyClock) retry(ctx context.Context, id uint64, owner string, cause error, retryAt int64) {
	now := s.now().UnixMilli()
	if retryAt < now {
		retryAt = now
	}
	result, err := s.eval(ctx, loyaltyRetryScript, []string{loyaltyScheduleKey(id), loyaltyDueKey, loyaltyClaimKey(id)},
		owner, strconv.FormatUint(id, 10), strconv.FormatInt(now, 10), strconv.FormatInt(retryAt, 10), cause.Error(), strconv.FormatInt(watchTickQuickRetry.Milliseconds(), 10), strconv.FormatInt(watchTickInterval.Milliseconds(), 10), strconv.Itoa(watchTickQuickRetries))
	if err != nil {
		s.log.Warn("loyalty: retry state unavailable", module.BIDField(id), zap.Error(err))
		return
	}
	streak, _ := result.AsInt64()
	if streak <= 0 {
		return
	}
	fields := []zap.Field{module.BIDField(id), zap.Int64("consecutive_failures", streak), zap.Error(cause)}
	if streak >= loyaltyEscalationLevel {
		s.log.Error("loyalty: watch window failing", fields...)
	} else {
		s.log.Warn("loyalty: watch window retry scheduled", fields...)
	}
}
func (s *ValkeyLoyaltyClock) chatterViewerID(raw string) (uint64, bool) {
	if raw == s.botID {
		return 0, false
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	return id, err == nil && id != 0
}

type chattersError struct {
	message      string
	missingScope bool
}

func (e *chattersError) Error() string {
	if e.missingScope {
		return "chatters unavailable (missing scope or moderator seat): " + e.message
	}
	return e.message
}
func rearmAfterFailure(failures int) time.Duration {
	if failures <= watchTickQuickRetries {
		return watchTickQuickRetry
	}
	return watchTickInterval
}

// Retained for event dedup callers/tests; production windows additionally carry
// their persisted session and due instant rather than a worker's local clock.
func watchTickIdentity(id uint64, at time.Time) string {
	return "wtick:" + strconv.FormatUint(id, 10) + ":" + strconv.FormatInt(at.Unix()/int64(watchTickInterval/time.Second), 10)
}
