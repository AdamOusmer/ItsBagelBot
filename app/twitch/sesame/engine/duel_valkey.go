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
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	pkg_valkey "ItsBagelBot/pkg/valkey"

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
	OpenerID    uint64   `json:"opener_id,omitempty"`
	OpenerStake int64    `json:"opener_stake"`
	Challenged  string   `json:"challenged,omitempty"`
	ChallengeID uint64   `json:"challenged_id,omitempty"`
	OpenedAt    int64    `json:"opened_at"`
}

func (st DuelState) openerStake() DuelStake {
	return DuelStake{Login: st.Opener, ViewerID: st.OpenerID, Stake: st.OpenerStake}
}

func (st DuelState) challengedParty() DuelParty {
	return DuelParty{ID: st.ChallengeID, Login: st.Challenged}
}

func (st DuelState) openerParty() DuelParty {
	return DuelParty{ID: st.OpenerID, Login: st.Opener}
}

func (p DuelParty) is(other DuelParty) bool {
	if p.ID != 0 && other.ID != 0 {
		return p.ID == other.ID
	}
	return p.Login == other.Login
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
	_, found, spent, err := w.loyalty.BalanceSpend(ctx, PointSpend{BroadcasterID: broadcasterID, ViewerID: entry.ViewerID, Login: entry.Login, Amount: entry.Stake})
	return found, spent, err
}

func (w *LoyaltyWallet) Credit(ctx context.Context, broadcasterID uint64, entry DuelStake) error {
	if entry.Stake <= 0 {
		return nil
	}
	bal, found, err := w.credit(ctx, broadcasterID, entry)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("duel: credit target %q unseen by loyalty (bal %+v)", entry.Login, bal)
	}
	return nil
}

func (w *LoyaltyWallet) credit(ctx context.Context, broadcasterID uint64, entry DuelStake) (loyaltyrpc.Balance, bool, error) {
	if entry.ViewerID == 0 {
		return w.loyalty.BalanceAdjust(ctx, broadcasterID, entry.Login, entry.Stake, false)
	}
	return w.loyalty.BalanceAdjustViewer(ctx, BalanceAdjustment{BroadcasterID: broadcasterID, ViewerID: entry.ViewerID, ViewerLogin: entry.Login, Value: entry.Stake})
}

type DuelOpenSpec struct {
	Kind             DuelKind
	Opener           DuelParty
	Challenged       DuelParty
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
	Join(ctx context.Context, broadcasterID uint64, who DuelParty, stake int64) (DuelJoinResult, error)
	Accept(ctx context.Context, broadcasterID uint64, who DuelParty) (DuelAcceptResult, error)
	Decline(ctx context.Context, broadcasterID uint64, who DuelParty) (DuelDeclineResult, error)
	Cancel(ctx context.Context, broadcasterID uint64, who DuelParty, moderator bool) (DuelCancelResult, error)
	Status(ctx context.Context, broadcasterID uint64) (DuelStatus, error)
	StartExpiryWatcher(ctx context.Context)
}

type DuelConfig struct {
	AnnouncementConfig
	Wallet DuelWallet
	Expiry *pkg_valkey.ExpiryHub
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

func (s *ValkeyDuelStore) holdSection(ctx context.Context, broadcasterID uint64) bool {
	won, err := pkg_valkey.ClaimOnce(ctx, s.client, duelKey(duelDrawPrefix, broadcasterID), duelDrawLockTTL)
	return err == nil && won
}

func (s *ValkeyDuelStore) releaseSection(ctx context.Context, broadcasterID uint64) {
	_ = pkg_valkey.DelKeys(ctx, s.client, duelKey(duelDrawPrefix, broadcasterID))
}

func (s *ValkeyDuelStore) Open(ctx context.Context, broadcasterID uint64, spec DuelOpenSpec) (DuelOpenResult, error) {
	res := DuelOpenResult{}

	seconds, err := prepareDuelOpen(spec)
	if err != nil {
		return res, err
	}
	won, err := pkg_valkey.NewOwnerLock(s.client, duelKey(duelDeadlinePrefix, broadcasterID), string(spec.Kind)).
		Acquire(ctx, time.Duration(seconds)*time.Second)
	if err != nil {
		return res, err
	}
	if !won {
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
	if spec.Opener.Login == "" || spec.Opener.ID == 0 {
		return fmt.Errorf("duel: open without an opener")
	}
	if spec.Kind == DuelChallenge {
		return validateChallenged(spec)
	}
	return validateKind(spec)
}

func validateChallenged(spec DuelOpenSpec) error {
	if spec.Challenged.Login == "" || spec.Challenged.ID == 0 {
		return fmt.Errorf("duel: challenge without a challenged party")
	}
	if spec.Opener.ID == spec.Challenged.ID {
		return fmt.Errorf("duel: self-challenge (%q)", spec.Opener.Login)
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
		Kind: spec.Kind, Opener: spec.Opener.Login, OpenerID: spec.Opener.ID, OpenerStake: spec.Stake,
		Challenged: spec.Challenged.Login, ChallengeID: spec.Challenged.ID, OpenedAt: time.Now().UnixMilli(),
	}

	found, spent, err := s.cfg.Wallet.Debit(ctx, broadcasterID, spec.Opener.stake(spec.Stake))
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
	var escrow *pkg_valkey.HashFieldTTL
	if spec.Kind == DuelPot {
		entry := spec.Opener.stake(spec.Stake)
		escrow = &pkg_valkey.HashFieldTTL{
			Key: duelKey(duelEntriesPrefix, broadcasterID), Field: entry.ledgerField(), Value: entry.ledgerValue(),
			TTL: duelStateTTL,
		}
	}
	return pkg_valkey.InstallWithEscrowField(ctx, s.client,
		pkg_valkey.Key{Name: duelKey(duelStatePrefix, broadcasterID), TTL: duelStateTTL}, string(blob), escrow)
}

func (s *ValkeyDuelStore) compensateOpen(ctx context.Context, broadcasterID uint64, state DuelState) {
	if err := s.cfg.Wallet.Credit(ctx, broadcasterID, state.openerStake()); err != nil {
		s.log.Warn("duel: open rollback refund failed", module.BIDField(broadcasterID),
			zap.String("login", state.Opener), zap.Int64("amount", state.OpenerStake), zap.Error(err))
	}
	s.releaseSlot(ctx, broadcasterID)
	_ = pkg_valkey.DelKeys(ctx, s.client, duelKey(duelEntriesPrefix, broadcasterID), duelKey(duelStatePrefix, broadcasterID))
}

func (s *ValkeyDuelStore) releaseSlot(ctx context.Context, broadcasterID uint64) {
	_ = pkg_valkey.DelKeys(ctx, s.client, duelKey(duelDeadlinePrefix, broadcasterID))
}

func (s *ValkeyDuelStore) Join(ctx context.Context, broadcasterID uint64, who DuelParty, stake int64) (DuelJoinResult, error) {
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
	return s.joinPot(ctx, broadcasterID, who.stake(stake), res)
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
		s.unclaimSeat(ctx, broadcasterID, entry)
		return res, err
	case !found:
		s.unclaimSeat(ctx, broadcasterID, entry)
		res.Unknown = true
		return s.joinTotals(ctx, broadcasterID, res)
	case !spent:
		s.unclaimSeat(ctx, broadcasterID, entry)
		res.Short = true
		return s.joinTotals(ctx, broadcasterID, res)
	}

	_ = pkg_valkey.Expire(ctx, s.client, duelKey(duelStatePrefix, broadcasterID), duelStateTTL)

	res.Joined = true
	return s.joinTotals(ctx, broadcasterID, res)
}

func (s *ValkeyDuelStore) claimSeat(ctx context.Context, broadcasterID uint64, entry DuelStake) (bool, error) {
	return pkg_valkey.HSetIfAbsent(ctx, s.client, duelKey(duelEntriesPrefix, broadcasterID), entry.ledgerField(), entry.ledgerValue())
}

func (s *ValkeyDuelStore) unclaimSeat(ctx context.Context, broadcasterID uint64, entry DuelStake) {
	if err := (pkg_valkey.HashField{Key: duelKey(duelEntriesPrefix, broadcasterID), Field: entry.ledgerField()}).Delete(ctx, s.client); err != nil {
		s.log.Warn("duel: join rollback unclaim failed", module.BIDField(broadcasterID),
			zap.String("login", entry.Login), zap.Error(err))
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
	vals, err := pkg_valkey.HValues(ctx, s.client, duelKey(duelEntriesPrefix, broadcasterID))
	if err != nil {
		return poolSummary{}, err
	}
	return poolSummary{entrants: int64(len(vals)), pot: sumStakes(vals)}, nil
}

func (s *ValkeyDuelStore) stateOf(ctx context.Context, broadcasterID uint64) *DuelState {
	st, ok, err := pkg_valkey.ReadJSON[DuelState](ctx, s.client, duelKey(duelStatePrefix, broadcasterID))
	if err != nil {
		s.log.Warn("duel: state read failed", module.BIDField(broadcasterID), zap.Error(err))
	}
	if !ok {
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

func (s *ValkeyDuelStore) Accept(ctx context.Context, broadcasterID uint64, who DuelParty) (DuelAcceptResult, error) {
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
	if !challengeAddressed(st, who) {
		res.WrongUser = true
		return res, nil
	}

	found, spent, err := s.cfg.Wallet.Debit(ctx, broadcasterID, who.stake(st.OpenerStake))
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
	winner, loser, pot := st.challengedParty(), st.openerParty(), st.OpenerStake*2
	if FlipDuelCoin() {
		winner, loser = loser, winner
	}
	receipt := DuelReceipt{
		Outcome: DuelWon, Winner: winner.Login, Loser: loser.Login, Pot: pot,
		ResolvedAt: time.Now().UnixMilli(),
	}
	s.teardown(ctx, broadcasterID, &receipt, true)
	return receipt, s.payWinner(ctx, broadcasterID, winner.stake(pot))
}

func (s *ValkeyDuelStore) payWinner(ctx context.Context, broadcasterID uint64, entry DuelStake) error {
	if err := s.cfg.Wallet.Credit(ctx, broadcasterID, entry); err != nil {
		s.log.Warn("duel: winner credit failed", module.BIDField(broadcasterID),
			zap.String("winner", entry.Login), zap.Int64("pot", entry.Stake), zap.Error(err))
		return err
	}
	return nil
}

func challengeAddressed(st *DuelState, who DuelParty) bool {
	return st.Kind == DuelChallenge && st.challengedParty().is(who)
}

func (s *ValkeyDuelStore) Decline(ctx context.Context, broadcasterID uint64, who DuelParty) (DuelDeclineResult, error) {
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
	if !challengeAddressed(st, who) {
		res.WrongUser = true
		return res, nil
	}

	refund := st.OpenerStake
	receipt := DuelReceipt{
		Outcome: DuelDeclined, Loser: st.Challenged, RefundTo: st.Opener, Pot: refund,
		ResolvedAt: time.Now().UnixMilli(),
	}
	s.teardown(ctx, broadcasterID, &receipt, false)
	s.refund(ctx, broadcasterID, st.openerStake())

	res.Declined = true
	res.Opener = st.Opener
	res.Refund = refund
	return res, nil
}

func (s *ValkeyDuelStore) Cancel(ctx context.Context, broadcasterID uint64, who DuelParty, moderator bool) (DuelCancelResult, error) {
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
	if !moderator && !st.openerParty().is(who) {
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
	fallback := []DuelStake{st.openerStake()}
	if st.Kind != DuelPot {
		return fallback, nil
	}
	m, err := pkg_valkey.HashAll(ctx, s.client, duelKey(duelEntriesPrefix, broadcasterID))
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return fallback, nil
	}
	return parseDuelLedger(m), nil
}

func (s *ValkeyDuelStore) refundAll(ctx context.Context, broadcasterID uint64, entries []DuelStake) (refunded int64) {
	for _, entry := range entries {
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
	raw, ok, err := pkg_valkey.ReadString(ctx, s.client, duelKey(duelStatePrefix, broadcasterID))
	if err != nil {
		return st, err
	}
	if !ok {
		return st, nil
	}
	state := DuelState{}
	if codec.UnmarshalFromString(raw, &state) != nil {
		return st, nil
	}
	left, err := pkg_valkey.TTLSeconds(ctx, s.client, duelKey(duelDeadlinePrefix, broadcasterID))
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
	lastKey := duelKey(duelLastPrefix, broadcasterID)

	blob, err := codec.Marshal(receipt)
	if err != nil {
		s.log.Warn("duel: receipt marshal failed", zap.Error(err))
	}
	var escrow *pkg_valkey.RenameTTL
	if receipt.SnapKey == "" && hasLedger {
		snap := duelSnapPrefix + strconv.FormatUint(broadcasterID, 10) + ":" + strconv.FormatInt(now, 10)
		escrow = &pkg_valkey.RenameTTL{From: duelKey(duelEntriesPrefix, broadcasterID), To: snap, TTL: duelReceiptTTL}
	}
	drop := []string{duelKey(duelStatePrefix, broadcasterID), duelKey(duelDeadlinePrefix, broadcasterID)}
	if err := pkg_valkey.CloseWithReceipt(ctx, s.client, pkg_valkey.Key{Name: lastKey, TTL: duelReceiptTTL}, string(blob), drop, escrow); err != nil {
		s.log.Warn("duel: teardown incomplete", module.BIDField(broadcasterID), zap.Error(err))
	}
}

func (s *ValkeyDuelStore) StartExpiryWatcher(ctx context.Context) {
	pkg_valkey.ExpiryHubOr(s.cfg.Expiry, s.client, 0, s.log).Watch(ctx, s.onExpired, duelDeadlinePrefix)
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
		pkg_valkey.RearmAndForget(dctx, s.client,
			pkg_valkey.Key{Name: duelKey(duelDeadlinePrefix, broadcasterID), TTL: 2 * time.Second}, "retry",
			duelKey(duelClaimPrefix, broadcasterID))
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
	m, err := pkg_valkey.HashAll(ctx, s.client, duelKey(duelEntriesPrefix, broadcasterID))
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
	payout := PickDuelStake(sorted, RollDuel(total))
	winner := payout.Login

	snap := duelSnapPrefix + strconv.FormatUint(broadcasterID, 10) + ":" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	receipt := DuelReceipt{
		Outcome: DuelWon, Winner: winner, Pot: total,
		Stakes: ledgerMap(sorted),
		Digest: DigestDuelPool(sorted), SnapKey: snap,
		ResolvedAt: time.Now().UnixMilli(),
	}
	s.teardownWithSnapshot(ctx, broadcasterID, &receipt)

	payout.Stake = total
	if err := s.cfg.Wallet.Credit(ctx, broadcasterID, payout); err != nil {
		s.log.Warn("duel: pot payout failed", module.BIDField(broadcasterID),
			zap.String("winner", winner), zap.Int64("pot", total), zap.Error(err))
	}
	s.announce(ctx, broadcasterID, func(v duelVoice) string { return duelAutoWonText(v, winner, total) })
}

func (s *ValkeyDuelStore) autoNoShow(ctx context.Context, broadcasterID uint64, st *DuelState) {
	receipt := DuelReceipt{
		Outcome: DuelNoShow, Loser: st.Challenged, RefundTo: st.Opener, Pot: st.OpenerStake,
		ResolvedAt: time.Now().UnixMilli(),
	}
	s.teardown(ctx, broadcasterID, &receipt, false)
	s.refund(ctx, broadcasterID, st.openerStake())
	s.announce(ctx, broadcasterID, func(v duelVoice) string { return duelNoShowText(v, st) })
}

func (s *ValkeyDuelStore) refundOnly(ctx context.Context, broadcasterID uint64, st *DuelState, outcome DuelOutcome) {
	receipt := DuelReceipt{Outcome: outcome, RefundTo: st.Opener, Pot: st.OpenerStake, ResolvedAt: time.Now().UnixMilli()}
	s.teardown(ctx, broadcasterID, &receipt, false)
	s.refund(ctx, broadcasterID, st.openerStake())
}

func (s *ValkeyDuelStore) teardownWithSnapshot(ctx context.Context, broadcasterID uint64, receipt *DuelReceipt) {
	blob, err := codec.Marshal(receipt)
	if err != nil {
		s.log.Warn("duel: receipt marshal failed", zap.Error(err))
	}
	escrow := pkg_valkey.RenameTTL{From: duelKey(duelEntriesPrefix, broadcasterID), To: receipt.SnapKey, TTL: duelReceiptTTL}
	drop := []string{duelKey(duelStatePrefix, broadcasterID), duelKey(duelDeadlinePrefix, broadcasterID)}
	lastKey := pkg_valkey.Key{Name: duelKey(duelLastPrefix, broadcasterID), TTL: duelReceiptTTL}
	if err := pkg_valkey.SnapshotThenReceipt(ctx, s.client, escrow, lastKey, string(blob), drop); err != nil {
		s.log.Warn("duel: snapshot teardown incomplete", module.BIDField(broadcasterID), zap.Error(err))
	}
}
