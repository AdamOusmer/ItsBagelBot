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
	"ItsBagelBot/internal/projection"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	duelDeadlinePrefix = "duel:deadline:"
	duelStatePrefix    = "duel:state:"
	duelEntriesPrefix  = "duel:entries:"
	duelClaimPrefix    = "duel:claim:"
	duelDrawPrefix     = "duel:draw:"
	duelSnapPrefix     = "duel:snap:"
	duelLastPrefix     = "duel:last:"
)

const (
	duelClaimTTL    = 5 * time.Second
	duelDrawLockTTL = 10 * time.Second
	duelStateTTL    = 12 * time.Hour
	duelReceiptTTL  = 24 * time.Hour
)

type DuelKind string

const (
	DuelPot       DuelKind = "pot"
	DuelChallenge DuelKind = "challenge"
)

type DuelState struct {
	Kind        DuelKind `json:"kind"`
	Opener      string   `json:"opener"`
	OpenerStake int64    `json:"opener_stake"`
	Challenged  string   `json:"challenged,omitempty"`
	OpenedAt    int64    `json:"opened_at"`
}

type DuelOutcome string

const (
	DuelWon      DuelOutcome = "won"
	DuelCanceled DuelOutcome = "cancelled"
	DuelDeclined DuelOutcome = "declined"
	DuelNoShow   DuelOutcome = "no_show"
)

type DuelReceipt struct {
	Outcome    DuelOutcome      `json:"outcome"`
	Winner     string           `json:"winner,omitempty"`
	Loser      string           `json:"loser,omitempty"`
	RefundTo   string           `json:"refund_to,omitempty"`
	Pot        int64            `json:"pot"`
	Stakes     map[string]int64 `json:"stakes,omitempty"`
	Digest     string           `json:"digest,omitempty"`
	SnapKey    string           `json:"snap_key,omitempty"`
	ResolvedAt int64            `json:"resolved_at"`
}

type DuelWallet interface {
	Debit(ctx context.Context, broadcasterID uint64, entry DuelStake) (found, spent bool, err error)
	Credit(ctx context.Context, broadcasterID uint64, entry DuelStake) error
}

// Debit must stay on balance.spend's conditional UPDATE so concurrent spends never go negative.
type LoyaltyWallet struct {
	loyalty LoyaltyStore
}

func NewLoyaltyWallet(loyalty LoyaltyStore) *LoyaltyWallet { return &LoyaltyWallet{loyalty: loyalty} }

func (w *LoyaltyWallet) Debit(ctx context.Context, broadcasterID uint64, entry DuelStake) (bool, bool, error) {
	_, found, spent, err := w.loyalty.BalanceSpend(ctx, broadcasterID, entry.Login, entry.Stake)
	return found, spent, err
}

func (w *LoyaltyWallet) Credit(ctx context.Context, broadcasterID uint64, entry DuelStake) error {
	if entry.Stake <= 0 {
		return nil
	}
	bal, found, err := w.loyalty.BalanceAdjust(ctx, broadcasterID, entry.Login, entry.Stake, false)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("duel: credit target %q unseen by loyalty (bal %+v)", entry.Login, bal)
	}
	return nil
}

type DuelOpenSpec struct {
	Kind             DuelKind
	Opener           string
	Challenged       string
	Stake            int64
	PotSeconds       int64
	ChallengeSeconds int64
}

type DuelOpenResult struct {
	Started bool
	Busy    bool
	Short   bool
	Unknown bool
}

type DuelJoinResult struct {
	Open             bool
	Joined           bool
	Already          bool
	Short            bool
	Unknown          bool
	Busy             bool
	ChallengePending bool
	Pot              int64
	Entrants         int64
}

type DuelAcceptResult struct {
	Found     bool
	WrongUser bool
	Busy      bool
	Short     bool
	Unknown   bool
	Accepted  bool
	Unpaid    bool
	Winner    string
	Loser     string
	Pot       int64
	Stake     int64
}

type DuelDeclineResult struct {
	Found     bool
	WrongUser bool
	Busy      bool
	Declined  bool
	Opener    string
	Refund    int64
}

type DuelCancelResult struct {
	Found     bool
	Allowed   bool
	Busy      bool
	Cancelled bool
	Refunded  int64
	Total     int64
}

type DuelStatus struct {
	Open        bool
	Kind        DuelKind
	Opener      string
	Challenged  string
	Stake       int64
	Pot         int64
	Entrants    int64
	SecondsLeft int64
}

type DuelStore interface {
	Open(ctx context.Context, broadcasterID uint64, spec DuelOpenSpec) (DuelOpenResult, error)
	Join(ctx context.Context, broadcasterID uint64, login string, stake int64) (DuelJoinResult, error)
	Accept(ctx context.Context, broadcasterID uint64, login string) (DuelAcceptResult, error)
	Decline(ctx context.Context, broadcasterID uint64, login string) (DuelDeclineResult, error)
	Cancel(ctx context.Context, broadcasterID uint64, byLogin string, moderator bool) (DuelCancelResult, error)
	Status(ctx context.Context, broadcasterID uint64) (DuelStatus, error)
	StartExpiryWatcher(ctx context.Context)
}

type DuelConfig struct {
	OutgressPremiumSubject  string
	OutgressStandardSubject string
	Pub                     bus.Publisher
	Proj                    projection.Reader
	Wallet                  DuelWallet
}

type ValkeyDuelStore struct {
	client valkey.Client
	cfg    DuelConfig
	log    *zap.Logger
}

func NewValkeyDuelStore(client valkey.Client, cfg DuelConfig, log *zap.Logger) *ValkeyDuelStore {
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyDuelStore{client: pkg_valkey.Primary(client), cfg: cfg, log: log}
}

func benign(err error) bool {
	return err == nil || valkey.IsValkeyNil(err)
}

func (s *ValkeyDuelStore) holdSection(ctx context.Context, broadcasterID uint64) bool {
	got, err := s.client.Do(ctx, s.client.B().Set().
		Key(duelKey(duelDrawPrefix, broadcasterID)).Value("1").
		Nx().PxMilliseconds(duelDrawLockTTL.Milliseconds()).Build()).ToString()
	if err != nil {
		return false
	}
	return got == "OK"
}

func (s *ValkeyDuelStore) releaseSection(ctx context.Context, broadcasterID uint64) {
	s.client.Do(ctx, s.client.B().Del().Key(duelKey(duelDrawPrefix, broadcasterID)).Build())
}

func (s *ValkeyDuelStore) Open(ctx context.Context, broadcasterID uint64, spec DuelOpenSpec) (DuelOpenResult, error) {
	res := DuelOpenResult{}

	seconds, err := prepareDuelOpen(spec)
	if err != nil {
		return res, err
	}
	got, err := s.client.Do(ctx, s.client.B().Set().
		Key(duelKey(duelDeadlinePrefix, broadcasterID)).Value(string(spec.Kind)).
		Nx().ExSeconds(seconds).Build()).ToString()
	if err != nil {
		return res, err
	}
	if got != "OK" {
		res.Busy = true
		return res, nil
	}
	return s.install(ctx, broadcasterID, spec)
}

func prepareDuelOpen(spec DuelOpenSpec) (int64, error) {
	if err := validateOpenSpec(spec); err != nil {
		return 0, err
	}
	return openClock(spec), nil
}

func validateOpenSpec(spec DuelOpenSpec) error {
	if spec.Stake <= 0 {
		return fmt.Errorf("duel: stake below the floor (%d)", spec.Stake)
	}
	if spec.Stake > DuelMaxStake {
		return fmt.Errorf("duel: stake above the ceiling (%d)", spec.Stake)
	}
	if spec.Opener == "" {
		return fmt.Errorf("duel: open without an opener")
	}
	if spec.Kind == DuelChallenge {
		return validateChallenged(spec)
	}
	return validateKind(spec)
}

func validateChallenged(spec DuelOpenSpec) error {
	if spec.Challenged == "" {
		return fmt.Errorf("duel: challenge without a challenged party")
	}
	if spec.Opener == spec.Challenged {
		return fmt.Errorf("duel: self-challenge (%q)", spec.Opener)
	}
	return nil
}

func validateKind(spec DuelOpenSpec) error {
	switch spec.Kind {
	case DuelPot:
		return nil
	case DuelChallenge:
		return nil
	default:
		return fmt.Errorf("duel: unknown kind %q", spec.Kind)
	}
}

func openClock(spec DuelOpenSpec) int64 {
	if spec.Kind == DuelChallenge {
		return ClampDuelSeconds(spec.ChallengeSeconds, DuelDefaultChallengeSeconds)
	}
	return ClampDuelSeconds(spec.PotSeconds, DuelDefaultPotSeconds)
}

func (s *ValkeyDuelStore) install(ctx context.Context, broadcasterID uint64, spec DuelOpenSpec) (DuelOpenResult, error) {
	res := DuelOpenResult{}
	state := DuelState{
		Kind: spec.Kind, Opener: spec.Opener, OpenerStake: spec.Stake,
		Challenged: spec.Challenged, OpenedAt: time.Now().UnixMilli(),
	}

	found, spent, err := s.cfg.Wallet.Debit(ctx, broadcasterID, DuelStake{Login: spec.Opener, Stake: spec.Stake})
	switch {
	case err != nil:
		s.releaseSlot(ctx, broadcasterID)
		return res, err
	case !found:
		s.releaseSlot(ctx, broadcasterID)
		res.Unknown = true
		return res, nil
	case !spent:
		s.releaseSlot(ctx, broadcasterID)
		res.Short = true
		return res, nil
	}

	blob, err := codec.Marshal(state)
	if err == nil {
		err = s.writeInstall(ctx, broadcasterID, spec, blob)
	}
	if err != nil {
		s.compensateOpen(ctx, broadcasterID, state)
		return res, err
	}
	res.Started = true
	return res, nil
}

func (s *ValkeyDuelStore) writeInstall(ctx context.Context, broadcasterID uint64, spec DuelOpenSpec, blob []byte) error {
	batch := []valkey.Completed{
		s.client.B().Set().Key(duelKey(duelStatePrefix, broadcasterID)).
			Value(string(blob)).ExSeconds(int64(duelStateTTL.Seconds())).Build(),
	}
	if spec.Kind == DuelPot {
		batch = append(batch,
			s.client.B().Hsetex().Key(duelKey(duelEntriesPrefix, broadcasterID)).
				Ex(int64(duelStateTTL.Seconds())).
				Fields().Numfields(1).FieldValue().
				FieldValue(spec.Opener, strconv.FormatInt(spec.Stake, 10)).
				Build())
	}
	for _, r := range s.client.DoMulti(ctx, batch...) {
		if err := r.Error(); err != nil && !valkey.IsValkeyNil(err) {
			return err
		}
	}
	return nil
}

func (s *ValkeyDuelStore) compensateOpen(ctx context.Context, broadcasterID uint64, state DuelState) {
	entry := DuelStake{Login: state.Opener, Stake: state.OpenerStake}
	if err := s.cfg.Wallet.Credit(ctx, broadcasterID, entry); err != nil {
		s.log.Warn("duel: open rollback refund failed", module.BIDField(broadcasterID),
			zap.String("login", state.Opener), zap.Int64("amount", state.OpenerStake), zap.Error(err))
	}
	s.releaseSlot(ctx, broadcasterID)
	s.client.Do(ctx, s.client.B().Del().
		Key(duelKey(duelEntriesPrefix, broadcasterID)).
		Key(duelKey(duelStatePrefix, broadcasterID)).Build())
}

func (s *ValkeyDuelStore) releaseSlot(ctx context.Context, broadcasterID uint64) {
	s.client.Do(ctx, s.client.B().Del().Key(duelKey(duelDeadlinePrefix, broadcasterID)).Build())
}

func (s *ValkeyDuelStore) Join(ctx context.Context, broadcasterID uint64, login string, stake int64) (DuelJoinResult, error) {
	res := DuelJoinResult{}
	if stake <= 0 || stake > DuelMaxStake {
		return res, fmt.Errorf("duel: join stake invalid (%d)", stake)
	}

	st, busy := s.beginResolution(ctx, broadcasterID)
	if busy {
		res.Busy = true
		return res, nil
	}
	defer s.releaseSection(ctx, broadcasterID)
	if st == nil {
		return res, nil
	}
	res.Open = true
	if st.Kind != DuelPot {
		res.ChallengePending = true
		return res, nil
	}
	return s.joinPot(ctx, broadcasterID, DuelStake{Login: login, Stake: stake}, res)
}

// Claim the seat before debiting so a duplicate join never moves points twice.
func (s *ValkeyDuelStore) joinPot(ctx context.Context, broadcasterID uint64, entry DuelStake, res DuelJoinResult) (DuelJoinResult, error) {
	added, err := s.claimSeat(ctx, broadcasterID, entry)
	if err != nil {
		return res, err
	}
	if !added {
		res.Already = true
		return s.joinTotals(ctx, broadcasterID, res)
	}

	found, spent, err := s.cfg.Wallet.Debit(ctx, broadcasterID, entry)
	switch {
	case err != nil:
		s.unclaimSeat(ctx, broadcasterID, entry.Login)
		return res, err
	case !found:
		s.unclaimSeat(ctx, broadcasterID, entry.Login)
		res.Unknown = true
		return s.joinTotals(ctx, broadcasterID, res)
	case !spent:
		s.unclaimSeat(ctx, broadcasterID, entry.Login)
		res.Short = true
		return s.joinTotals(ctx, broadcasterID, res)
	}

	s.client.Do(ctx, s.client.B().Expire().
		Key(duelKey(duelStatePrefix, broadcasterID)).Seconds(int64(duelStateTTL.Seconds())).Build())

	res.Joined = true
	return s.joinTotals(ctx, broadcasterID, res)
}

func (s *ValkeyDuelStore) claimSeat(ctx context.Context, broadcasterID uint64, entry DuelStake) (bool, error) {
	added, err := s.client.Do(ctx, s.client.B().Hsetnx().
		Key(duelKey(duelEntriesPrefix, broadcasterID)).
		Field(entry.Login).Value(strconv.FormatInt(entry.Stake, 10)).Build()).AsInt64()
	return added > 0, err
}

func (s *ValkeyDuelStore) unclaimSeat(ctx context.Context, broadcasterID uint64, login string) {
	if _, err := s.hdelEntry(ctx, broadcasterID, login); err != nil {
		s.log.Warn("duel: join rollback unclaim failed", module.BIDField(broadcasterID),
			zap.String("login", login), zap.Error(err))
	}
}

type poolSummary struct {
	entrants int64
	pot      int64
}

func (s *ValkeyDuelStore) joinTotals(ctx context.Context, broadcasterID uint64, res DuelJoinResult) (DuelJoinResult, error) {
	pool, err := s.readLedger(ctx, broadcasterID)
	if err != nil {
		return res, err
	}
	res.Entrants = pool.entrants
	res.Pot = pool.pot
	return res, nil
}

func (s *ValkeyDuelStore) readLedger(ctx context.Context, broadcasterID uint64) (poolSummary, error) {
	vals, err := s.client.Do(ctx, s.client.B().Hvals().
		Key(duelKey(duelEntriesPrefix, broadcasterID)).Build()).AsStrSlice()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return poolSummary{}, nil
		}
		return poolSummary{}, err
	}
	return poolSummary{entrants: int64(len(vals)), pot: sumStakes(vals)}, nil
}

func (s *ValkeyDuelStore) hdelEntry(ctx context.Context, broadcasterID uint64, login string) (int64, error) {
	return s.client.Do(ctx, s.client.B().Hdel().
		Key(duelKey(duelEntriesPrefix, broadcasterID)).Field(login).Build()).AsInt64()
}

func (s *ValkeyDuelStore) stateOf(ctx context.Context, broadcasterID uint64) *DuelState {
	raw, err := s.client.Do(ctx, s.client.B().Get().
		Key(duelKey(duelStatePrefix, broadcasterID)).Build()).ToString()
	if err != nil {
		if !valkey.IsValkeyNil(err) {
			s.log.Warn("duel: state read failed", module.BIDField(broadcasterID), zap.Error(err))
		}
		return nil
	}
	st := DuelState{}
	if codec.UnmarshalFromString(raw, &st) != nil {
		s.log.Warn("duel: unreadable state", module.BIDField(broadcasterID))
		return nil
	}
	return &st
}

func (s *ValkeyDuelStore) beginResolution(ctx context.Context, broadcasterID uint64) (*DuelState, bool) {
	if !s.holdSection(ctx, broadcasterID) {
		return nil, true
	}
	return s.stateOf(ctx, broadcasterID), false
}

func (s *ValkeyDuelStore) Accept(ctx context.Context, broadcasterID uint64, login string) (DuelAcceptResult, error) {
	res := DuelAcceptResult{}
	st, busy := s.beginResolution(ctx, broadcasterID)
	if busy {
		res.Busy = true
		return res, nil
	}
	defer s.releaseSection(ctx, broadcasterID)
	if st == nil {
		return res, nil
	}
	res.Found = true
	if !challengeAddressed(st, login) {
		res.WrongUser = true
		return res, nil
	}

	found, spent, err := s.cfg.Wallet.Debit(ctx, broadcasterID, DuelStake{Login: login, Stake: st.OpenerStake})
	switch {
	case err != nil:
		return res, err
	case !found:
		res.Unknown = true
		return res, nil
	case !spent:
		res.Short = true
		return res, nil
	}

	receipt, payErr := s.settleChallenge(ctx, broadcasterID, st)
	if payErr != nil {
		res.Unpaid = true
	}
	res.Accepted = true
	res.Winner, res.Loser, res.Pot, res.Stake = receipt.Winner, receipt.Loser, receipt.Pot, st.OpenerStake
	return res, nil
}

func (s *ValkeyDuelStore) settleChallenge(ctx context.Context, broadcasterID uint64, st *DuelState) (DuelReceipt, error) {
	winner, loser, pot := st.Challenged, st.Opener, st.OpenerStake*2
	if FlipDuelCoin() {
		winner, loser = st.Opener, st.Challenged
	}
	receipt := DuelReceipt{
		Outcome: DuelWon, Winner: winner, Loser: loser, Pot: pot,
		ResolvedAt: time.Now().UnixMilli(),
	}
	s.teardown(ctx, broadcasterID, &receipt, true)
	return receipt, s.payWinner(ctx, broadcasterID, &receipt)
}

func (s *ValkeyDuelStore) payWinner(ctx context.Context, broadcasterID uint64, receipt *DuelReceipt) error {
	entry := DuelStake{Login: receipt.Winner, Stake: receipt.Pot}
	if err := s.cfg.Wallet.Credit(ctx, broadcasterID, entry); err != nil {
		s.log.Warn("duel: winner credit failed", module.BIDField(broadcasterID),
			zap.String("winner", receipt.Winner), zap.Int64("pot", receipt.Pot), zap.Error(err))
		return err
	}
	return nil
}

func challengeAddressed(st *DuelState, login string) bool {
	return st.Kind == DuelChallenge && login == st.Challenged
}

func (s *ValkeyDuelStore) Decline(ctx context.Context, broadcasterID uint64, login string) (DuelDeclineResult, error) {
	res := DuelDeclineResult{}
	st, busy := s.beginResolution(ctx, broadcasterID)
	if busy {
		res.Busy = true
		return res, nil
	}
	defer s.releaseSection(ctx, broadcasterID)
	if st == nil {
		return res, nil
	}
	res.Found = true
	if !challengeAddressed(st, login) {
		res.WrongUser = true
		return res, nil
	}

	refund := st.OpenerStake
	receipt := DuelReceipt{
		Outcome: DuelDeclined, Loser: st.Challenged, RefundTo: st.Opener, Pot: refund,
		ResolvedAt: time.Now().UnixMilli(),
	}
	s.teardown(ctx, broadcasterID, &receipt, false)
	s.refund(ctx, broadcasterID, DuelStake{Login: st.Opener, Stake: refund})

	res.Declined = true
	res.Opener = st.Opener
	res.Refund = refund
	return res, nil
}

func (s *ValkeyDuelStore) Cancel(ctx context.Context, broadcasterID uint64, byLogin string, moderator bool) (DuelCancelResult, error) {
	res := DuelCancelResult{}
	st, busy := s.beginResolution(ctx, broadcasterID)
	if busy {
		res.Busy = true
		return res, nil
	}
	defer s.releaseSection(ctx, broadcasterID)
	if st == nil {
		return res, nil
	}
	res.Found = true
	if !moderator && byLogin != st.Opener {
		return res, nil
	}

	entries, err := s.escrowedStakes(ctx, broadcasterID, st)
	if err != nil {
		// A ledger read failure must not become an opener-only refund; leave the duel alive.
		s.log.Warn("duel: cancel ledger read failed", module.BIDField(broadcasterID), zap.Error(err))
		return res, err
	}

	total := sumEntries(entries)
	// Receipt and teardown before paying, so a second cancel finds nothing to double-refund.
	receipt := DuelReceipt{
		Outcome: DuelCanceled, Stakes: ledgerMap(entries), Pot: total,
		ResolvedAt: time.Now().UnixMilli(),
	}
	s.teardown(ctx, broadcasterID, &receipt, len(entries) > 0)
	res.Refunded = s.refundAll(ctx, broadcasterID, entries)
	res.Cancelled = true
	res.Total = total
	return res, nil
}

func sumEntries(entries []DuelStake) int64 {
	var total int64
	for _, e := range entries {
		total += e.Stake
	}
	return total
}

func (s *ValkeyDuelStore) escrowedStakes(ctx context.Context, broadcasterID uint64, st *DuelState) ([]DuelStake, error) {
	fallback := []DuelStake{{Login: st.Opener, Stake: st.OpenerStake}}
	if st.Kind != DuelPot {
		return fallback, nil
	}
	m, err := s.client.Do(ctx, s.client.B().Hgetall().
		Key(duelKey(duelEntriesPrefix, broadcasterID)).Build()).AsStrMap()
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return fallback, nil
	}
	return parseDuelLedger(m), nil
}

func (s *ValkeyDuelStore) refundAll(ctx context.Context, broadcasterID uint64, entries []DuelStake) (refunded int64) {
	for _, entry := range SortDuelStakes(entries) {
		if err := s.cfg.Wallet.Credit(ctx, broadcasterID, entry); err != nil {
			s.log.Warn("duel: cancel refund failed", module.BIDField(broadcasterID),
				zap.String("login", entry.Login), zap.Int64("amount", entry.Stake), zap.Error(err))
			continue
		}
		refunded++
	}
	return refunded
}

func (s *ValkeyDuelStore) refund(ctx context.Context, broadcasterID uint64, entry DuelStake) {
	if err := s.cfg.Wallet.Credit(ctx, broadcasterID, entry); err != nil {
		s.log.Warn("duel: refund failed", module.BIDField(broadcasterID),
			zap.String("login", entry.Login), zap.Int64("amount", entry.Stake), zap.Error(err))
	}
}

func (s *ValkeyDuelStore) Status(ctx context.Context, broadcasterID uint64) (DuelStatus, error) {
	st := DuelStatus{SecondsLeft: -1}
	raw, err := s.client.Do(ctx, s.client.B().Get().
		Key(duelKey(duelStatePrefix, broadcasterID)).Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return st, nil
		}
		return st, err
	}
	state := DuelState{}
	if codec.UnmarshalFromString(raw, &state) != nil {
		return st, nil
	}
	left, err := s.client.Do(ctx, s.client.B().Ttl().
		Key(duelKey(duelDeadlinePrefix, broadcasterID)).Build()).AsInt64()
	if err != nil {
		return st, err
	}
	st.Open = left > 0
	st.Kind = state.Kind
	st.Opener = state.Opener
	st.Challenged = state.Challenged
	st.Stake = state.OpenerStake
	st.SecondsLeft = left
	if state.Kind == DuelChallenge {
		st.Entrants = 2
		st.Pot = state.OpenerStake
		return st, nil
	}
	pool, err := s.readLedger(ctx, broadcasterID)
	if err != nil {
		return st, err
	}
	st.Entrants = pool.entrants
	st.Pot = pool.pot
	return st, nil
}

func (s *ValkeyDuelStore) teardown(ctx context.Context, broadcasterID uint64, receipt *DuelReceipt, hasLedger bool) {
	now := receipt.ResolvedAt
	ttl := int64(duelReceiptTTL.Seconds())
	lastKey := duelKey(duelLastPrefix, broadcasterID)

	blob, err := codec.Marshal(receipt)
	if err != nil {
		s.log.Warn("duel: receipt marshal failed", zap.Error(err))
	}
	batch := []valkey.Completed{
		s.client.B().Set().Key(lastKey).Value(string(blob)).ExSeconds(ttl).Build(),
		s.client.B().Del().Key(duelKey(duelStatePrefix, broadcasterID)).
			Key(duelKey(duelDeadlinePrefix, broadcasterID)).Build(),
	}
	if receipt.SnapKey == "" && hasLedger {
		snap := duelSnapPrefix + strconv.FormatUint(broadcasterID, 10) + ":" + strconv.FormatInt(now, 10)
		batch = append(batch,
			s.client.B().Rename().Key(duelKey(duelEntriesPrefix, broadcasterID)).Newkey(snap).Build(),
			s.client.B().Expire().Key(snap).Seconds(ttl).Build())
	}
	for _, r := range s.client.DoMulti(ctx, batch...) {
		if err := r.Error(); !benign(err) {
			s.log.Warn("duel: teardown incomplete", module.BIDField(broadcasterID), zap.Error(err))
			break
		}
	}
}

func (s *ValkeyDuelStore) StartExpiryWatcher(ctx context.Context) {
	channel := "__keyevent@0__:expired"
	s.log.Info("duel: expiry watcher starting", zap.String("channel", channel))

	for ctx.Err() == nil {
		err := s.client.Receive(ctx, s.client.B().Subscribe().Channel(channel).Build(), func(msg valkey.PubSubMessage) {
			s.onExpired(ctx, msg.Message)
		})
		if ctx.Err() != nil {
			return
		}
		s.log.Warn("duel: expiry watcher dropped, reconnecting", zap.Error(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (s *ValkeyDuelStore) onExpired(ctx context.Context, key string) {
	if !strings.HasPrefix(key, duelDeadlinePrefix) {
		return
	}
	id, ok := parseRaffleID(strings.TrimPrefix(key, duelDeadlinePrefix))
	if !ok {
		return
	}
	if s.claimExpiry(ctx, duelKey(duelClaimPrefix, id)) {
		go s.autoResolve(context.WithoutCancel(ctx), id)
	}
}

func (s *ValkeyDuelStore) claimExpiry(ctx context.Context, key string) bool {
	won, _ := pkg_valkey.ClaimOnce(ctx, s.client, key, duelClaimTTL)
	return won
}

func (s *ValkeyDuelStore) autoResolve(ctx context.Context, broadcasterID uint64) {
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if !s.holdSection(dctx, broadcasterID) {
		// The claim delete is required: otherwise the retry expires inside the claim TTL and is dropped.
		s.client.DoMulti(dctx,
			s.client.B().Set().
				Key(duelKey(duelDeadlinePrefix, broadcasterID)).Value("retry").ExSeconds(2).Build(),
			s.client.B().Del().Key(duelKey(duelClaimPrefix, broadcasterID)).Build())
		return
	}
	defer s.releaseSection(dctx, broadcasterID)

	st := s.stateOf(dctx, broadcasterID)
	if st == nil {
		return
	}
	switch st.Kind {
	case DuelChallenge:
		s.autoNoShow(dctx, broadcasterID, st)
	default:
		s.autoDraw(dctx, broadcasterID, st)
	}
}

func (s *ValkeyDuelStore) autoDraw(ctx context.Context, broadcasterID uint64, st *DuelState) {
	m, err := s.client.Do(ctx, s.client.B().Hgetall().
		Key(duelKey(duelEntriesPrefix, broadcasterID)).Build()).AsStrMap()
	if err != nil {
		s.log.Warn("duel: auto-draw ledger read failed", module.BIDField(broadcasterID), zap.Error(err))
		return
	}
	sorted := parseDuelLedger(m)
	if len(sorted) == 0 {
		s.refundOnly(ctx, broadcasterID, st, DuelCanceled)
		return
	}
	total := int64(0)
	for _, e := range sorted {
		total += e.Stake
	}
	winner := PickDuelWinner(sorted, RollDuel(total))

	snap := duelSnapPrefix + strconv.FormatUint(broadcasterID, 10) + ":" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	receipt := DuelReceipt{
		Outcome: DuelWon, Winner: winner, Pot: total,
		Stakes: ledgerMap(sorted),
		Digest: DigestDuelPool(sorted), SnapKey: snap,
		ResolvedAt: time.Now().UnixMilli(),
	}
	s.teardownWithSnapshot(ctx, broadcasterID, &receipt)

	if err := s.cfg.Wallet.Credit(ctx, broadcasterID, DuelStake{Login: winner, Stake: total}); err != nil {
		s.log.Warn("duel: pot payout failed", module.BIDField(broadcasterID),
			zap.String("winner", winner), zap.Int64("pot", total), zap.Error(err))
	}
	s.announce(ctx, broadcasterID, func(locale string) string {
		return expandTokens(module.Locale(locale), tokenExpansion{
			text: i18nT(locale, "duel.auto_won"),
			kv:   []string{"user", winner, "amount", strconv.FormatInt(total, 10)},
		})
	})
}

func (s *ValkeyDuelStore) autoNoShow(ctx context.Context, broadcasterID uint64, st *DuelState) {
	receipt := DuelReceipt{
		Outcome: DuelNoShow, Loser: st.Challenged, RefundTo: st.Opener, Pot: st.OpenerStake,
		ResolvedAt: time.Now().UnixMilli(),
	}
	s.teardown(ctx, broadcasterID, &receipt, false)
	s.refund(ctx, broadcasterID, DuelStake{Login: st.Opener, Stake: st.OpenerStake})
	s.announce(ctx, broadcasterID, func(locale string) string {
		return expandTokens(module.Locale(locale), tokenExpansion{
			text: i18nT(locale, "duel.auto_noshow"),
			kv: []string{
				"opener", st.Opener, "target", st.Challenged,
				"amount", strconv.FormatInt(st.OpenerStake, 10),
			},
		})
	})
}

func (s *ValkeyDuelStore) refundOnly(ctx context.Context, broadcasterID uint64, st *DuelState, outcome DuelOutcome) {
	receipt := DuelReceipt{Outcome: outcome, RefundTo: st.Opener, Pot: st.OpenerStake, ResolvedAt: time.Now().UnixMilli()}
	s.teardown(ctx, broadcasterID, &receipt, false)
	s.refund(ctx, broadcasterID, DuelStake{Login: st.Opener, Stake: st.OpenerStake})
}

func (s *ValkeyDuelStore) teardownWithSnapshot(ctx context.Context, broadcasterID uint64, receipt *DuelReceipt) {
	snap := receipt.SnapKey
	ttl := int64(duelReceiptTTL.Seconds())
	blob, err := codec.Marshal(receipt)
	if err != nil {
		s.log.Warn("duel: receipt marshal failed", zap.Error(err))
	}
	for _, r := range s.client.DoMulti(ctx,
		s.client.B().Rename().Key(duelKey(duelEntriesPrefix, broadcasterID)).Newkey(snap).Build(),
		s.client.B().Expire().Key(snap).Seconds(ttl).Build(),
		s.client.B().Set().Key(duelKey(duelLastPrefix, broadcasterID)).Value(string(blob)).ExSeconds(ttl).Build(),
		s.client.B().Del().Key(duelKey(duelStatePrefix, broadcasterID)).
			Key(duelKey(duelDeadlinePrefix, broadcasterID)).Build(),
	) {
		if err := r.Error(); !benign(err) {
			s.log.Warn("duel: snapshot teardown incomplete", module.BIDField(broadcasterID), zap.Error(err))
			break
		}
	}
}
