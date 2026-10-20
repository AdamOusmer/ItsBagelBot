// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	pkg_valkey "ItsBagelBot/pkg/valkey"

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
	raffleNamesPrefix    = "raffle:names:"
	raffleDueKey         = "raffle:due"
)

const (
	raffleClaimTTL      = 5 * time.Second
	raffleDrawLockTTL   = 10 * time.Second
	raffleStateTTL      = 12 * time.Hour
	raffleReceiptTTL    = 24 * time.Hour
	raffleClaimWindow   = 15 * time.Minute
	raffleSweepInterval = 30 * time.Second
	raffleSweepGrace    = 5 * time.Second
	raffleSweepBatch    = 100
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
	Prize         int64  `json:"prize,omitempty"`
}

type RaffleResult struct {
	Winners  []RaffleWinner `json:"winners"`
	Entrants int64          `json:"entrants"`
	Prize    int64          `json:"prize,omitempty"`
	Payout   RafflePayout   `json:"payout,omitempty"`
	Digest   string         `json:"digest"`
	DrawnAt  int64          `json:"drawn_at"`
	Claims   []string       `json:"-"`
}

type RaffleOpenSpec struct {
	OpenedBy string
	Winners  int64
	Duration time.Duration
	Remind   time.Duration
	Prize    int64
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
	Join(ctx context.Context, broadcasterID uint64, entrant RaffleEntrant) (RaffleEntry, error)
	Status(ctx context.Context, broadcasterID uint64) (RaffleStatus, error)
	Draw(ctx context.Context, broadcasterID uint64, winners int64) (*RaffleResult, error)
	Cancel(ctx context.Context, broadcasterID uint64) (ok bool, err error)
	LastResult(ctx context.Context, broadcasterID uint64) (*RaffleResult, bool, error)
	Claim(ctx context.Context, broadcasterID uint64, viewerID string) (RaffleClaim, error)
	StartExpiryWatcher(ctx context.Context)
}

type RaffleConfig struct {
	AnnouncementConfig
	Expiry *pkg_valkey.ExpiryHub
	Wallet RaffleWallet
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
	return pkg_valkey.ClaimOnce(ctx, s.client, raffleKey(raffleDeadlinePrefix, broadcasterID), duration.Truncate(time.Second))
}

type raffleInstall struct {
	state      []byte
	remindSecs int64
	dueAt      time.Time
}

func (s *ValkeyRaffleStore) installState(ctx context.Context, broadcasterID uint64, in raffleInstall) error {
	state, remindSecs, dueAt := in.state, in.remindSecs, in.dueAt
	batch := []valkey.Completed{
		s.client.B().Set().Key(raffleKey(raffleStatePrefix, broadcasterID)).
			Value(string(state)).ExSeconds(int64(raffleStateTTL.Seconds())).Build(),
		s.client.B().Del().Key(raffleKey(raffleEntriesPrefix, broadcasterID)).
			Key(raffleKey(raffleNamesPrefix, broadcasterID)).Build(),
		s.client.B().Zadd().Key(raffleDueKey).ScoreMember().
			ScoreMember(float64(dueAt.UnixMilli()), strconv.FormatUint(broadcasterID, 10)).Build(),
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

	now := time.Now()
	state, err := codec.Marshal(RaffleState{
		OpenedBy: spec.OpenedBy, OpenedAt: now.UnixMilli(),
		Winners: spec.Winners, RemindSeconds: remindSecs, Prize: spec.Prize,
	})
	if err != nil {
		_ = s.client.Do(ctx, s.client.B().Del().
			Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).Build()).Error()
		return false, err
	}
	if err := s.installState(ctx, broadcasterID, raffleInstall{state: state, remindSecs: remindSecs, dueAt: now.Add(spec.Duration)}); err != nil {
		return false, err
	}
	return true, nil
}

// The deadline check matters: once it expires the pool is about to be snapshotted.
var joinScript = valkey.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 0 or redis.call('EXISTS', KEYS[2]) == 0 then
  return {0, 0, redis.call('ZCARD', KEYS[3])}
end
local added = redis.call('ZADD', KEYS[3], 'NX', ARGV[1], ARGV[2])
redis.call('HSET', KEYS[4], ARGV[2], ARGV[3])
redis.call('EXPIRE', KEYS[3], ARGV[4])
redis.call('EXPIRE', KEYS[4], ARGV[4])
redis.call('EXPIRE', KEYS[1], ARGV[4])
return {1, added, redis.call('ZCARD', KEYS[3])}
`)

func (s *ValkeyRaffleStore) Join(ctx context.Context, broadcasterID uint64, entrant RaffleEntrant) (RaffleEntry, error) {
	if entrant.ID == "" {
		return RaffleEntry{}, nil
	}
	reply, err := joinScript.Exec(ctx, s.client,
		[]string{
			raffleKey(raffleStatePrefix, broadcasterID), raffleKey(raffleDeadlinePrefix, broadcasterID),
			raffleKey(raffleEntriesPrefix, broadcasterID), raffleKey(raffleNamesPrefix, broadcasterID),
		},
		[]string{
			strconv.FormatInt(time.Now().UnixMilli(), 10), entrant.ID, entrant.Login,
			strconv.FormatInt(int64(raffleStateTTL.Seconds()), 10),
		},
	).AsIntSlice()
	if err != nil {
		return RaffleEntry{}, err
	}
	if len(reply) != 3 {
		return RaffleEntry{}, fmt.Errorf("raffle: join script returned %d values", len(reply))
	}
	return RaffleEntry{Open: reply[0] == 1, Joined: reply[1] > 0, Entrants: reply[2]}, nil
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
	resps := s.client.DoMulti(ctx,
		s.client.B().Del().
			Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).
			Key(raffleKey(raffleStatePrefix, broadcasterID)).
			Key(raffleKey(raffleRemindPrefix, broadcasterID)).
			Key(raffleKey(raffleEntriesPrefix, broadcasterID)).
			Key(raffleKey(raffleNamesPrefix, broadcasterID)).Build(),
		s.forgetDueCmd(broadcasterID),
	)
	n, err := resps[0].AsInt64()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *ValkeyRaffleStore) forgetDueCmd(broadcasterID uint64) valkey.Completed {
	return s.client.B().Zrem().Key(raffleDueKey).Member(strconv.FormatUint(broadcasterID, 10)).Build()
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
	won, err := pkg_valkey.ClaimOnce(ctx, s.client, raffleKey(raffleDrawPrefix, broadcasterID), raffleDrawLockTTL)
	return err == nil && won
}

type drawRead struct {
	State   RaffleState
	Count   int64
	Members []string
	Names   map[string]string
}

func (s *ValkeyRaffleStore) readDrawPhase(ctx context.Context, broadcasterID uint64, override int64) (*drawRead, error) {
	resps := s.client.DoMulti(ctx,
		s.client.B().Get().Key(raffleKey(raffleStatePrefix, broadcasterID)).Build(),
		s.client.B().Zrange().Key(raffleKey(raffleEntriesPrefix, broadcasterID)).Min("0").Max("-1").Build(),
		s.client.B().Hgetall().Key(raffleKey(raffleNamesPrefix, broadcasterID)).Build(),
	)
	v, err := resps[0].ToString()
	if valkey.IsValkeyNil(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	read := &drawRead{}
	_ = codec.UnmarshalFromString(v, &read.State)
	read.Count = read.State.Winners
	if override > 0 {
		read.Count = override
	}
	if read.Members, err = resps[1].AsStrSlice(); err != nil {
		return nil, err
	}
	read.Names, _ = resps[2].AsStrMap()
	return read, nil
}

func (r *drawRead) result(drawnAt int64) *RaffleResult {
	picked := pickWinners(r.Members, max(r.Count, raffleDefaultWinners))
	shares := splitPrize(r.State.Prize, len(picked))
	winners := make([]RaffleWinner, len(picked))
	for i, id := range picked {
		winners[i] = RaffleWinner{ID: id, Login: r.loginOf(id), Points: shares[i]}
	}
	return &RaffleResult{
		Winners:  winners,
		Entrants: int64(len(r.Members)),
		Prize:    r.State.Prize,
		Digest:   DigestPool(r.Members),
		DrawnAt:  drawnAt,
	}
}

func (r *drawRead) loginOf(id string) string {
	if login := r.Names[id]; login != "" {
		return login
	}
	return id
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
			Key(raffleKey(raffleRemindPrefix, broadcasterID)).
			Key(raffleKey(raffleNamesPrefix, broadcasterID)).Build(),
		s.forgetDueCmd(broadcasterID),
		s.client.B().Hset().Key(receipt).FieldValue().FieldValue("result", marshalJSON(res)).Build(),
		s.client.B().Expire().Key(receipt).Seconds(ttl).Build(),
	) {
		if err := r.Error(); err != nil && !valkey.IsValkeyNil(err) {
			s.log.Warn("raffle: draw teardown incomplete", module.BIDField(broadcasterID), zap.Error(err))
			break
		}
	}
}

// Teardown must precede payout so no second draw can pay the same pool.
func (s *ValkeyRaffleStore) Draw(ctx context.Context, broadcasterID uint64, winners int64) (*RaffleResult, error) {
	if !s.holdDraw(ctx, broadcasterID) {
		return nil, nil
	}

	read, err := s.readDrawPhase(ctx, broadcasterID, winners)
	if err != nil || read == nil {
		return nil, err
	}

	res := read.result(time.Now().UnixMilli())
	s.writeDrawPhase(ctx, broadcasterID, res)
	s.payWinners(ctx, broadcasterID, res)
	s.recordPayout(ctx, broadcasterID, res)
	return res, nil
}

func (s *ValkeyRaffleStore) recordPayout(ctx context.Context, broadcasterID uint64, res *RaffleResult) {
	if res.Payout == RafflePayoutNone {
		return
	}
	err := s.client.Do(ctx, s.client.B().Hset().Key(raffleKey(raffleLastPrefix, broadcasterID)).
		FieldValue().FieldValue("result", marshalJSON(res)).Build()).Error()
	if err != nil {
		s.log.Warn("raffle: payout receipt not recorded", module.BIDField(broadcasterID), zap.Error(err))
	}
}

// Expiry events are lost while no replica is subscribed; the due sweep still draws and pays those raffles.
func (s *ValkeyRaffleStore) StartExpiryWatcher(ctx context.Context) {
	go s.sweepOverdue(ctx)
	pkg_valkey.ExpiryHubOr(s.cfg.Expiry, s.client, 0, s.log).Watch(ctx, s.onExpired, raffleDeadlinePrefix, raffleRemindPrefix)
}

func (s *ValkeyRaffleStore) sweepOverdue(ctx context.Context) {
	ticker := time.NewTicker(raffleSweepInterval)
	defer ticker.Stop()
	for {
		s.drawOverdue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *ValkeyRaffleStore) drawOverdue(ctx context.Context) {
	cutoff := time.Now().Add(-raffleSweepGrace).UnixMilli()
	ids, err := s.client.Do(ctx, s.client.B().Zrangebyscore().Key(raffleDueKey).
		Min("-inf").Max(strconv.FormatInt(cutoff, 10)).Limit(0, raffleSweepBatch).Build()).AsStrSlice()
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("raffle: overdue sweep failed", zap.Error(err))
		}
		return
	}
	for _, idStr := range ids {
		s.onExpired(ctx, raffleDeadlinePrefix+idStr)
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
