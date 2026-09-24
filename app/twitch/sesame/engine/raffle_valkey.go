// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	raffleDeadlinePrefix = "raffle:deadline:"
	raffleStatePrefix    = "raffle:state:"
	raffleEntriesPrefix  = "raffle:entries:"
	raffleClaimPrefix    = "raffle:claim:"
	raffleDrawPrefix     = "raffle:draw:"
	raffleSnapPrefix     = "raffle:snap:"
	raffleLastPrefix     = "raffle:last:"
	raffleRemindPrefix   = "raffle:remind:"
	raffleRClaimPrefix   = "raffle:rclaim:"
)

const (
	raffleClaimTTL    = 5 * time.Second
	raffleDrawLockTTL = 10 * time.Second
	raffleStateTTL    = 12 * time.Hour
	raffleReceiptTTL  = 24 * time.Hour
	raffleClaimWindow = 15 * time.Minute
)

const (
	minRaffleDuration    = time.Minute
	maxRaffleDuration    = 2 * time.Hour
	maxRaffleWinners     = 20
	raffleDefaultWinners = 1
	raffleDefaultRemind  = int64(5 * time.Minute / time.Second)
	minRaffleRemind      = int64(time.Minute / time.Second)
)

type RaffleState struct {
	OpenedBy      string `json:"opened_by"`
	OpenedAt      int64  `json:"opened_at"`
	Winners       int64  `json:"winners"`
	RemindSeconds int64  `json:"remind_s"`
}

type RaffleResult struct {
	Winners  []string `json:"winners"`
	Entrants int64    `json:"entrants"`
	Digest   string   `json:"digest"`
	DrawnAt  int64    `json:"drawn_at"`
	Claims   []string `json:"-"`
}

type RaffleOpenSpec struct {
	OpenedBy string
	Winners  int64
	Duration time.Duration
	Remind   time.Duration
}

type RaffleEntry struct {
	Joined   bool
	Open     bool
	Entrants int64
}

type RaffleStatus struct {
	Open        bool
	Entrants    int64
	SecondsLeft int64
}

type RaffleStore interface {
	Open(ctx context.Context, broadcasterID uint64, spec RaffleOpenSpec) (ok bool, err error)
	Join(ctx context.Context, broadcasterID uint64, userID string) (RaffleEntry, error)
	Status(ctx context.Context, broadcasterID uint64) (RaffleStatus, error)
	Draw(ctx context.Context, broadcasterID uint64, winners int64) (*RaffleResult, error)
	Cancel(ctx context.Context, broadcasterID uint64) (ok bool, err error)
	LastResult(ctx context.Context, broadcasterID uint64) (*RaffleResult, bool, error)
	Claim(ctx context.Context, broadcasterID uint64, userID string) (RaffleClaim, error)
	StartExpiryWatcher(ctx context.Context)
}

type RaffleConfig struct {
	OutgressPremiumSubject  string
	OutgressStandardSubject string
	Pub                     bus.Publisher
	Proj                    projection.Reader
}

type ValkeyRaffleStore struct {
	client valkey.Client
	cfg    RaffleConfig
	log    *zap.Logger
}

func NewValkeyRaffleStore(client valkey.Client, cfg RaffleConfig, log *zap.Logger) *ValkeyRaffleStore {
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyRaffleStore{client: pkg_valkey.Primary(client), cfg: cfg, log: log}
}

func (s *ValkeyRaffleStore) armDeadline(ctx context.Context, broadcasterID uint64, duration time.Duration) (bool, error) {
	got, err := s.client.Do(ctx, s.client.B().Set().
		Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).
		Value("1").
		Nx().ExSeconds(int64(duration.Seconds())).Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil
		}
		return false, err
	}
	return got == "OK", nil
}

func (s *ValkeyRaffleStore) installState(ctx context.Context, broadcasterID uint64, state []byte, remindSecs int64) error {
	batch := []valkey.Completed{
		s.client.B().Set().Key(raffleKey(raffleStatePrefix, broadcasterID)).
			Value(string(state)).ExSeconds(int64(raffleStateTTL.Seconds())).Build(),
		s.client.B().Del().Key(raffleKey(raffleEntriesPrefix, broadcasterID)).Build(),
	}
	if remindSecs > 0 {
		batch = append(batch,
			s.client.B().Set().Key(raffleKey(raffleRemindPrefix, broadcasterID)).
				Value("1").ExSeconds(remindSecs).Build())
	}
	for _, r := range s.client.DoMulti(ctx, batch...) {
		if err := r.Error(); err != nil && !valkey.IsValkeyNil(err) {
			_ = s.client.Do(ctx, s.client.B().Del().
				Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).Build()).Error()
			return err
		}
	}
	return nil
}

func (s *ValkeyRaffleStore) Open(ctx context.Context, broadcasterID uint64, spec RaffleOpenSpec) (bool, error) {
	spec, remindSecs := clampRaffleOpen(spec)

	ok, err := s.armDeadline(ctx, broadcasterID, spec.Duration)
	if err != nil || !ok {
		return false, err
	}

	state, err := codec.Marshal(RaffleState{
		OpenedBy: spec.OpenedBy, OpenedAt: time.Now().UnixMilli(),
		Winners: spec.Winners, RemindSeconds: remindSecs,
	})
	if err != nil {
		_ = s.client.Do(ctx, s.client.B().Del().
			Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).Build()).Error()
		return false, err
	}
	if err := s.installState(ctx, broadcasterID, state, remindSecs); err != nil {
		return false, err
	}
	return true, nil
}

func (s *ValkeyRaffleStore) Join(ctx context.Context, broadcasterID uint64, userID string) (RaffleEntry, error) {
	key := raffleKey(raffleEntriesPrefix, broadcasterID)
	seconds := int64(raffleStateTTL.Seconds())
	resps := s.client.DoMulti(ctx,
		s.client.B().Exists().Key(raffleKey(raffleStatePrefix, broadcasterID)).Build(),
		s.client.B().Zadd().Key(key).Nx().ScoreMember().ScoreMember(float64(time.Now().UnixMilli()), userID).Build(),
		s.client.B().Zcard().Key(key).Build(),
		s.client.B().Expire().Key(key).Seconds(seconds).Build(),
		s.client.B().Expire().Key(raffleKey(raffleStatePrefix, broadcasterID)).Seconds(seconds).Build(),
	)
	entry := RaffleEntry{}
	exists, err := resps[0].AsInt64()
	if err != nil {
		return entry, err
	}
	entry.Entrants, err = resps[2].AsInt64()
	if err != nil {
		return entry, err
	}
	if exists == 0 {
		return entry, nil
	}
	added, err := resps[1].AsInt64()
	if err != nil {
		entry.Open = true
		return entry, err
	}
	entry.Open = true
	entry.Joined = added > 0
	return entry, nil
}

func (s *ValkeyRaffleStore) Status(ctx context.Context, broadcasterID uint64) (RaffleStatus, error) {
	resps := s.client.DoMulti(ctx,
		s.client.B().Exists().Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).Build(),
		s.client.B().Zcard().Key(raffleKey(raffleEntriesPrefix, broadcasterID)).Build(),
		s.client.B().Ttl().Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).Build(),
	)
	st := RaffleStatus{SecondsLeft: -1}
	open, err := resps[0].AsInt64()
	if err != nil {
		return st, err
	}
	if st.Entrants, err = resps[1].AsInt64(); err != nil {
		return st, err
	}
	if st.SecondsLeft, err = resps[2].AsInt64(); err != nil {
		return st, err
	}
	st.Open = open > 0
	return st, nil
}

func (s *ValkeyRaffleStore) Cancel(ctx context.Context, broadcasterID uint64) (bool, error) {
	n, err := s.client.Do(ctx, s.client.B().Del().
		Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).
		Key(raffleKey(raffleStatePrefix, broadcasterID)).
		Key(raffleKey(raffleRemindPrefix, broadcasterID)).
		Key(raffleKey(raffleEntriesPrefix, broadcasterID)).Build()).AsInt64()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *ValkeyRaffleStore) LastResult(ctx context.Context, broadcasterID uint64) (*RaffleResult, bool, error) {
	m, err := s.client.Do(ctx, s.client.B().Hgetall().Key(raffleKey(raffleLastPrefix, broadcasterID)).Build()).AsStrMap()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	res, ok := decodeReceipt(m["result"], m["claims"])
	return res, ok, nil
}

func decodeReceipt(resultJSON, claimsJSON string) (*RaffleResult, bool) {
	var res RaffleResult
	if codec.UnmarshalFromString(resultJSON, &res) != nil {
		return nil, false
	}
	if claimsJSON != "" && codec.UnmarshalFromString(claimsJSON, &res.Claims) != nil {
		res.Claims = nil
	}
	return &res, true
}

func (s *ValkeyRaffleStore) holdDraw(ctx context.Context, broadcasterID uint64) bool {
	got, err := s.client.Do(ctx, s.client.B().Set().Key(raffleKey(raffleDrawPrefix, broadcasterID)).Value("1").
		Nx().PxMilliseconds(raffleDrawLockTTL.Milliseconds()).Build()).ToString()
	if err != nil {
		return false
	}
	return got == "OK"
}

type drawRead struct {
	Count   int64
	Members []string
}

func (s *ValkeyRaffleStore) readDrawPhase(ctx context.Context, broadcasterID uint64, override int64) (*drawRead, error) {
	read := &drawRead{Count: override}
	if override <= 0 {
		v, err := s.client.Do(ctx, s.client.B().Get().
			Key(raffleKey(raffleStatePrefix, broadcasterID)).Build()).ToString()
		if valkey.IsValkeyNil(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		st := RaffleState{}
		if codec.UnmarshalFromString(v, &st) != nil {
			return read, nil
		}
		read.Count = st.Winners
	}
	members, err := s.client.Do(ctx, s.client.B().Zrange().
		Key(raffleKey(raffleEntriesPrefix, broadcasterID)).Min("0").Max("-1").Build()).AsStrSlice()
	if err != nil {
		return nil, err
	}
	read.Members = members
	return read, nil
}

func (s *ValkeyRaffleStore) writeDrawPhase(ctx context.Context, broadcasterID uint64, res *RaffleResult) {
	now := res.DrawnAt
	snapKey := raffleSnapPrefix + strconv.FormatUint(broadcasterID, 10) + ":" + strconv.FormatInt(now, 10)
	receipt := raffleKey(raffleLastPrefix, broadcasterID)
	ttl := int64(raffleReceiptTTL.Seconds())
	for _, r := range s.client.DoMulti(ctx,
		s.client.B().Rename().Key(raffleKey(raffleEntriesPrefix, broadcasterID)).Newkey(snapKey).Build(),
		s.client.B().Expire().Key(snapKey).Seconds(ttl).Build(),
		s.client.B().Del().Key(raffleKey(raffleStatePrefix, broadcasterID)).
			Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).
			Key(raffleKey(raffleRemindPrefix, broadcasterID)).Build(),
		s.client.B().Hset().Key(receipt).FieldValue().FieldValue("result", marshalJSON(res)).Build(),
		s.client.B().Expire().Key(receipt).Seconds(ttl).Build(),
	) {
		if err := r.Error(); err != nil && !valkey.IsValkeyNil(err) {
			s.log.Warn("raffle: draw teardown incomplete", module.BIDField(broadcasterID), zap.Error(err))
			break
		}
	}
}

func (s *ValkeyRaffleStore) Draw(ctx context.Context, broadcasterID uint64, winners int64) (*RaffleResult, error) {
	if !s.holdDraw(ctx, broadcasterID) {
		return nil, nil
	}

	read, err := s.readDrawPhase(ctx, broadcasterID, winners)
	if err != nil || read == nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	res := &RaffleResult{
		Winners:  pickWinners(read.Members, read.Count),
		Entrants: int64(len(read.Members)),
		Digest:   DigestPool(read.Members),
		DrawnAt:  now,
	}
	s.writeDrawPhase(ctx, broadcasterID, res)
	return res, nil
}

func (s *ValkeyRaffleStore) StartExpiryWatcher(ctx context.Context) {
	channel := "__keyevent@0__:expired"
	s.log.Info("raffle: expiry watcher starting", zap.String("channel", channel))

	for ctx.Err() == nil {
		err := s.client.Receive(ctx, s.client.B().Subscribe().Channel(channel).Build(), func(msg valkey.PubSubMessage) {
			s.onExpired(ctx, msg.Message)
		})
		if ctx.Err() != nil {
			return
		}
		s.log.Warn("raffle: expiry watcher dropped, reconnecting", zap.Error(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (s *ValkeyRaffleStore) onExpired(ctx context.Context, key string) {
	switch {
	case strings.HasPrefix(key, raffleDeadlinePrefix):
		idStr := strings.TrimPrefix(key, raffleDeadlinePrefix)
		if id, ok := parseRaffleID(idStr); ok {
			if s.claimExpiry(ctx, raffleKey(raffleClaimPrefix, id)) {
				go s.autoDraw(context.WithoutCancel(ctx), id)
			}
		}
	case strings.HasPrefix(key, raffleRemindPrefix):
		idStr := strings.TrimPrefix(key, raffleRemindPrefix)
		if id, ok := parseRaffleID(idStr); ok {
			if s.claimExpiry(ctx, raffleKey(raffleRClaimPrefix, id)) {
				go s.remindTick(context.WithoutCancel(ctx), id)
			}
		}
	}
}

func parseRaffleID(s string) (uint64, bool) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

func (s *ValkeyRaffleStore) claimExpiry(ctx context.Context, key string) bool {
	won, _ := pkg_valkey.ClaimOnce(ctx, s.client, key, raffleClaimTTL)
	return won
}
