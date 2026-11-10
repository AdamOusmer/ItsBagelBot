// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
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
	minRaffleDuration    = 5 * time.Second
	maxRaffleDuration    = 2 * time.Hour
	maxRaffleWinners     = 100
	raffleDefaultWinners = 1
	raffleDefaultRemind  = int64(5 * time.Minute / time.Second)
)

type RaffleState struct {
	OpenedBy string `json:"opened_by"`
	OpenedAt int64  `json:"opened_at"`
	Prize    int64  `json:"prize,omitempty"`
	Multi    bool   `json:"multi,omitempty"`
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
	Duration time.Duration
	Prize    int64
	Multi    bool
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
	stateKey := pkg_valkey.Key{Name: raffleKey(raffleStatePrefix, broadcasterID), TTL: raffleStateTTL}
	clear := []string{raffleKey(raffleEntriesPrefix, broadcasterID), raffleKey(raffleNamesPrefix, broadcasterID)}
	var remind *pkg_valkey.Key
	if remindSecs > 0 {
		remind = &pkg_valkey.Key{Name: raffleKey(raffleRemindPrefix, broadcasterID), TTL: time.Duration(remindSecs) * time.Second}
	}
	err := pkg_valkey.OpenSession(ctx, s.client, pkg_valkey.OpenSpec{
		State: stateKey, StateValue: string(state), Clear: clear,
		Due: pkg_valkey.DueSchedule{
			Key: raffleDueKey, Score: float64(dueAt.UnixMilli()), Member: strconv.FormatUint(broadcasterID, 10),
		},
		Remind: remind, RemindValue: "1",
	})
	if err != nil {
		_ = pkg_valkey.DelKeys(ctx, s.client, raffleKey(raffleDeadlinePrefix, broadcasterID))
		return err
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
		Prize: spec.Prize, Multi: spec.Multi,
	})
	if err != nil {
		_ = pkg_valkey.DelKeys(ctx, s.client, raffleKey(raffleDeadlinePrefix, broadcasterID))
		return false, err
	}
	if err := s.installState(ctx, broadcasterID, raffleInstall{state: state, remindSecs: remindSecs, dueAt: now.Add(spec.Duration)}); err != nil {
		return false, err
	}
	return true, nil
}

func (s *ValkeyRaffleStore) Join(ctx context.Context, broadcasterID uint64, entrant RaffleEntrant) (RaffleEntry, error) {
	if entrant.ID == "" {
		return RaffleEntry{}, nil
	}
	gate := pkg_valkey.GatedJoin{
		GuardA: raffleKey(raffleStatePrefix, broadcasterID), GuardB: raffleKey(raffleDeadlinePrefix, broadcasterID),
		Set: raffleKey(raffleEntriesPrefix, broadcasterID), Labels: raffleKey(raffleNamesPrefix, broadcasterID),
	}
	open, added, count, err := gate.Join(ctx, s.client, pkg_valkey.Entrant{
		Score: time.Now().UnixMilli(), Member: entrant.ID, Label: entrant.Login, TTL: raffleStateTTL,
	})
	if err != nil {
		return RaffleEntry{}, err
	}
	return RaffleEntry{Open: open, Joined: added, Entrants: count}, nil
}

func (s *ValkeyRaffleStore) Status(ctx context.Context, broadcasterID uint64) (RaffleStatus, error) {
	st := RaffleStatus{SecondsLeft: -1}
	open, entrants, left, err := pkg_valkey.StatusProbe(ctx, s.client,
		raffleKey(raffleDeadlinePrefix, broadcasterID), raffleKey(raffleEntriesPrefix, broadcasterID))
	if err != nil {
		return st, err
	}
	st.Open, st.Entrants, st.SecondsLeft = open, entrants, left
	return st, nil
}

func (s *ValkeyRaffleStore) Cancel(ctx context.Context, broadcasterID uint64) (bool, error) {
	drop := []string{
		raffleKey(raffleDeadlinePrefix, broadcasterID), raffleKey(raffleStatePrefix, broadcasterID),
		raffleKey(raffleRemindPrefix, broadcasterID), raffleKey(raffleEntriesPrefix, broadcasterID),
		raffleKey(raffleNamesPrefix, broadcasterID),
	}
	due := pkg_valkey.DueMember{Key: raffleDueKey, Member: strconv.FormatUint(broadcasterID, 10)}
	return pkg_valkey.CancelAndForget(ctx, s.client, drop, due)
}

func (s *ValkeyRaffleStore) LastResult(ctx context.Context, broadcasterID uint64) (*RaffleResult, bool, error) {
	m, err := pkg_valkey.HashAll(ctx, s.client, raffleKey(raffleLastPrefix, broadcasterID))
	if err != nil {
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
	v, ok, members, names, err := pkg_valkey.ReadDrawPool(ctx, s.client, pkg_valkey.DrawPoolKeys{
		State: raffleKey(raffleStatePrefix, broadcasterID), Pool: raffleKey(raffleEntriesPrefix, broadcasterID), Labels: raffleKey(raffleNamesPrefix, broadcasterID),
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	read := &drawRead{Members: members, Names: names}
	_ = codec.UnmarshalFromString(v, &read.State)
	read.Count = read.winnerCount(override)
	return read, nil
}

func (r *drawRead) winnerCount(override int64) int64 {
	switch {
	case override > 0:
		return override
	case r.State.Multi:
		return multiRaffleWinners(len(r.Members))
	}
	return raffleDefaultWinners
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
	escrow := pkg_valkey.RenameTTL{From: raffleKey(raffleEntriesPrefix, broadcasterID), To: snapKey, TTL: raffleReceiptTTL}
	drop := []string{
		raffleKey(raffleStatePrefix, broadcasterID), raffleKey(raffleDeadlinePrefix, broadcasterID),
		raffleKey(raffleRemindPrefix, broadcasterID), raffleKey(raffleNamesPrefix, broadcasterID),
	}
	err := pkg_valkey.SnapshotThenFieldReceipt(ctx, s.client, pkg_valkey.SnapshotFieldReceiptSpec{
		Escrow: escrow, Drop: drop,
		Due: pkg_valkey.DueMember{Key: raffleDueKey, Member: strconv.FormatUint(broadcasterID, 10)},
		Receipt: pkg_valkey.HashFieldTTL{
			Key: raffleKey(raffleLastPrefix, broadcasterID), Field: "result", Value: marshalJSON(res), TTL: raffleReceiptTTL,
		},
	})
	if err != nil {
		s.log.Warn("raffle: draw teardown incomplete", module.BIDField(broadcasterID), zap.Error(err))
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
	field := pkg_valkey.HashField{Key: raffleKey(raffleLastPrefix, broadcasterID), Field: "result"}
	err := field.Set(ctx, s.client, marshalJSON(res))
	if err != nil {
		s.log.Warn("raffle: payout receipt not recorded", module.BIDField(broadcasterID), zap.Error(err))
	}
}

// Expiry events are lost while no replica is subscribed; the due sweep still draws and pays those raffles.
func (s *ValkeyRaffleStore) StartExpiryWatcher(ctx context.Context) {
	go sweepEvery(ctx, raffleSweepInterval, s.drawOverdue)
	pkg_valkey.ExpiryHubOr(s.cfg.Expiry, s.client, 0, s.log).Watch(ctx, s.onExpired, raffleDeadlinePrefix, raffleRemindPrefix)
}

func (s *ValkeyRaffleStore) drawOverdue(ctx context.Context) {
	cutoff := time.Now().Add(-raffleSweepGrace).UnixMilli()
	ids, err := pkg_valkey.ZRangeByScore(ctx, s.client, raffleDueKey,
		pkg_valkey.ScoreRange{Min: "-inf", Max: strconv.FormatInt(cutoff, 10), Limit: raffleSweepBatch})
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
