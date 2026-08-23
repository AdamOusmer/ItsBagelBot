// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/projection"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// Duel keyspace, one active duel per broadcaster (either flavor shares the
// slot — the deadline key is the single gate):
//
//	duel:deadline:<id>  the clock — a key EX'd to the pot window or the
//	                    challenge accept window whose expiry IS the
//	                    auto-resolution (the raffle/timer idiom). Its NX SET at
//	                    open enforces one-active-per-channel.
//	duel:state:<id>     the duel record (kind, opener, opener stake,
//	                    challenged party), JSON.
//	duel:entries:<id>   the pot's escrow ledger — a hash of login -> staked
//	                    points, opener seeded at open, so every refund and the
//	                    weighted pick read one authoritative structure.
//	duel:claim:<id>     guards one expiry so only one replica auto-resolves.
//	duel:draw:<id>      the resolution lock (SET NX PX), serializing every
//	                    money-moving path against each other.
//	duel:snap:<id>:<ts> the ledger renamed aside at resolve time — the exact
//	                    pool that produced the winner survives the dispute
//	                    window instead of vanishing with the duel.
//	duel:last:<id>      the receipt: outcome, winner, pot, stakes, digest.
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
	// duelClaimTTL matches raffleClaimTTL: long enough to cover the resolve
	// that follows, short enough that a crashed claimant only mutes one
	// auto-resolution.
	duelClaimTTL = 5 * time.Second
	// duelDrawLockTTL bounds any money-moving critical section (join, accept,
	// decline, cancel, resolve). A claimant that dies mid-section releases the
	// channel after this; joins take and release it explicitly because they
	// recur, unlike a draw.
	duelDrawLockTTL = 10 * time.Second
	// duelStateTTL reclaims a duel abandoned without resolution (every expiry
	// watcher replica down through the whole window). Re-armed on every join,
	// so an actively joined duel never expires mid-stream.
	duelStateTTL = 12 * time.Hour
	// duelReceiptTTL bounds how long the receipt and the ledger snapshot
	// survive after a resolve. This is the dispute window: past it the
	// evidence is gone and the duel is forgotten (no database by design).
	duelReceiptTTL = 24 * time.Hour
)

// DuelKind separates the two flavors sharing one store.
type DuelKind string

const (
	DuelPot       DuelKind = "pot"       // everyone stakes into a pool, weighted draw at close
	DuelChallenge DuelKind = "challenge" // two parties stake equal amounts, coin flip
)

// DuelState is the live duel's record. The opener's stake rides here rather
// than only in the ledger because the challenge flow has no ledger — and the
// no-show/cancel refunds need it even there.
type DuelState struct {
	Kind        DuelKind `json:"kind"`
	Opener      string   `json:"opener"`
	OpenerStake int64    `json:"opener_stake"`
	Challenged  string   `json:"challenged,omitempty"` // challenge kind only
	OpenedAt    int64    `json:"opened_at"`            // unix millis
}

// DuelOutcome names how a duel ended, as stored in the receipt.
type DuelOutcome string

const (
	DuelWon      DuelOutcome = "won"
	DuelCanceled DuelOutcome = "cancelled"
	DuelDeclined DuelOutcome = "declined"
	DuelNoShow   DuelOutcome = "no_show"
)

// DuelReceipt is one completed duel. Stakes/Digest/SnapKey bind the announced
// result to the exact pool that produced it (the raffle receipt idiom); a
// challenge keeps Stakes nil and carries Winner/Loser instead.
type DuelReceipt struct {
	Outcome    DuelOutcome      `json:"outcome"`
	Winner     string           `json:"winner,omitempty"`
	Loser      string           `json:"loser,omitempty"`
	Pot        int64            `json:"pot"`
	Stakes     map[string]int64 `json:"stakes,omitempty"`
	Digest     string           `json:"digest,omitempty"`
	SnapKey    string           `json:"snap_key,omitempty"`
	ResolvedAt int64            `json:"resolved_at"` // unix millis
}

// DuelWallet moves the points a duel escrows. It exists so the store — which
// owns every money-moving critical section — can call the conditional spend
// without importing the loyalty surface directly; sesame wires
// NewLoyaltyWallet over engine.LoyaltyStore, tests wire fakes.
type DuelWallet interface {
	// Debit takes amount points from login's balance, refusing (spent=false)
	// when they hold less. found=false means the channel never accrued for
	// them — they cannot play yet.
	Debit(ctx context.Context, broadcasterID uint64, login string, amount int64) (found, spent bool, err error)
	// Credit returns amount points to login (refunds and payouts). A credit
	// failing is logged loudly by the store: the receipt outlives it, so the
	// payment is reconstructible, but chat was told a different story.
	Credit(ctx context.Context, broadcasterID uint64, login string, amount int64) error
}

// LoyaltyWallet adapts the loyalty surface to DuelWallet. The debit rides
// balance.spend — the service-side conditional UPDATE — so two concurrent
// spends racing one row can never drive points negative.
type LoyaltyWallet struct {
	loyalty LoyaltyStore
}

// NewLoyaltyWallet builds the production wallet over the shared loyalty store.
func NewLoyaltyWallet(loyalty LoyaltyStore) *LoyaltyWallet { return &LoyaltyWallet{loyalty: loyalty} }

func (w *LoyaltyWallet) Debit(ctx context.Context, broadcasterID uint64, login string, amount int64) (bool, bool, error) {
	_, found, spent, err := w.loyalty.BalanceSpend(ctx, broadcasterID, login, amount)
	return found, spent, err
}

func (w *LoyaltyWallet) Credit(ctx context.Context, broadcasterID uint64, login string, amount int64) error {
	if amount <= 0 {
		return nil
	}
	bal, found, err := w.loyalty.BalanceAdjust(ctx, broadcasterID, login, amount, false)
	if err != nil {
		return err
	}
	if !found {
		// Unreachable for anyone who was debited moments earlier, but a
		// silent no-op here would eat a payout — make it visible.
		return fmt.Errorf("duel: credit target %q unseen by loyalty (bal %+v)", login, bal)
	}
	return nil
}

// DuelOpenSpec is one Open request. Stake is what the opener escrows (and
// what the challenged party must match); seconds bound whichever clock the
// kind arms, clamped store-side.
type DuelOpenSpec struct {
	Kind             DuelKind
	Opener           string
	Challenged       string // challenge kind only
	Stake            int64
	PotSeconds       int64
	ChallengeSeconds int64
}

// DuelOpenResult reports one open attempt. Started=false with Busy means the
// channel's duel slot was taken; Short/Unknown mean the opener could not
// escrow (nothing moved).
type DuelOpenResult struct {
	Started bool
	Busy    bool
	Short   bool
	Unknown bool
}

// DuelJoinResult reports one "!duel <points>" against a running duel.
// Nothing moves unless Joined comes back true; ChallengePending marks the
// blocked-join case where a challenge holds the slot.
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

// DuelAcceptResult reports one "!duel accept". Accepted=true carries the full
// outcome; Short/Unknown mean the acceptor could not match the stake.
type DuelAcceptResult struct {
	Found     bool
	WrongUser bool
	Busy      bool
	Short     bool
	Unknown   bool
	Accepted  bool
	Winner    string
	Loser     string
	Pot       int64
	Stake     int64
}

// DuelDeclineResult reports one "!duel decline" by the challenged party;
// Refund is what went back to Opener.
type DuelDeclineResult struct {
	Found     bool
	WrongUser bool
	Busy      bool
	Declined  bool
	Opener    string
	Refund    int64
}

// DuelCancelResult reports one cancel (opener or mod): who got refunded and
// how much moved back in total.
type DuelCancelResult struct {
	Found     bool
	Allowed   bool
	Busy      bool
	Cancelled bool
	Refunded  int64
	Total     int64
}

// DuelStatus is the bare "!duel" view. For a challenge, Pot is the opener's
// stake (the winner takes twice that once accepted) and Entrants is 2; for a
// pot, Pot is the live pool and Entrants its size. SecondsLeft mirrors TTL
// semantics: -1 when nothing runs.
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

// DuelStore is the per-broadcaster duel surface behind the duel module: one
// active duel, funded entirely by escrowed points, resolved by deadline
// expiry, acceptance, decline or cancellation — never by trust.
type DuelStore interface {
	// Open starts a pot duel (everyone adds stakes until the window closes,
	// then a stake-weighted draw pays one winner the pool) or a challenge
	// (the named party must accept within the window; a fair flip pays the
	// winner both stakes). ok=false on the result means the slot was busy.
	Open(ctx context.Context, broadcasterID uint64, spec DuelOpenSpec) (DuelOpenResult, error)
	// Join adds login's stake to a running pot duel. Already=true with
	// Joined=false means one entry per viewer.
	Join(ctx context.Context, broadcasterID uint64, login string, stake int64) (DuelJoinResult, error)
	// Accept matches the challenged party's stake and resolves immediately:
	// the winner is credited the whole pot before this returns.
	Accept(ctx context.Context, broadcasterID uint64, login string) (DuelAcceptResult, error)
	// Decline refuses a pending challenge; the opener's stake goes back.
	Decline(ctx context.Context, broadcasterID uint64, login string) (DuelDeclineResult, error)
	// Cancel tears a duel down with full refunds. Allowed is the opener or a
	// moderator; anything else is refused without touching points.
	Cancel(ctx context.Context, broadcasterID uint64, byLogin string, moderator bool) (DuelCancelResult, error)
	// Status reads the live duel for the bare command.
	Status(ctx context.Context, broadcasterID uint64) (DuelStatus, error)
	// StartExpiryWatcher subscribes to Valkey key-expiry notifications and
	// auto-resolves each duel whose deadline expires (pot: weighted draw;
	// challenge: refund the stood-up opener). Same deployment requirements as
	// the raffle/timer/loyalty watchers.
	StartExpiryWatcher(ctx context.Context)
}

// DuelConfig carries everything the store needs beyond the Valkey client and
// the wallet: where engine-posted announcements go and who resolves a
// broadcaster's tier and locale on those paths.
type DuelConfig struct {
	OutgressPremiumSubject  string
	OutgressStandardSubject string
	Pub                     bus.Publisher
	Proj                    projection.Reader
	Wallet                  DuelWallet
}

// ValkeyDuelStore implements DuelStore on the shared Valkey client,
// primary-consistent like the raffle store: one broadcaster's chat drives the
// whole duel in sequence, so a replica-lagging Status would contradict the
// Join that preceded it.
type ValkeyDuelStore struct {
	client valkey.Client
	cfg    DuelConfig
	log    *zap.Logger
}

// NewValkeyDuelStore builds the store on a primary-consistent view.
func NewValkeyDuelStore(client valkey.Client, cfg DuelConfig, log *zap.Logger) *ValkeyDuelStore {
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyDuelStore{client: pkg_valkey.Primary(client), cfg: cfg, log: log}
}

// validStake accepts exactly the ledger entries worth counting.
func validStake(n int64, err error) bool {
	return err == nil && n > 0
}

// readable reports whether a hash read failed outright or came back empty —
// both mean the opener-seeded fallback stands.
func readable(err error, m map[string]string) bool {
	return err != nil || len(m) == 0
}

// benign reports whether a reply's error is a normal outcome for these
// flows: either success or the empty reply the nil-guard idiom covers.
func benign(err error) bool {
	return err == nil || valkey.IsValkeyNil(err)
}

// holdSection takes the resolution lock. Every money-moving path runs inside
// it, so the read/mutate phases can never interleave across replicas.
func (s *ValkeyDuelStore) holdSection(ctx context.Context, broadcasterID uint64) bool {
	got, err := s.client.Do(ctx, s.client.B().Set().
		Key(duelKey(duelDrawPrefix, broadcasterID)).Value("1").
		Nx().PxMilliseconds(duelDrawLockTTL.Milliseconds()).Build()).ToString()
	if err != nil {
		return false
	}
	return got == "OK"
}

// releaseSection drops the lock early. Joins recur constantly, so they do not
// wait out the lock TTL; a crashed holder is still bounded by it.
func (s *ValkeyDuelStore) releaseSection(ctx context.Context, broadcasterID uint64) {
	s.client.Do(ctx, s.client.B().Del().Key(duelKey(duelDrawPrefix, broadcasterID)).Build())
}

func (s *ValkeyDuelStore) Open(ctx context.Context, broadcasterID uint64, spec DuelOpenSpec) (DuelOpenResult, error) {
	res := DuelOpenResult{}

	seconds, err := prepareDuelOpen(spec)
	if err != nil {
		return res, err
	}
	// Claim the channel's slot first: exactly one opener wins the NX, and a
	// loser learns so before anything moved.
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

// prepareDuelOpen validates one open request and resolves its clock: the pot
// window or the challenge accept window, clamped store-side.
func prepareDuelOpen(spec DuelOpenSpec) (int64, error) {
	if err := validateOpenSpec(spec); err != nil {
		return 0, err
	}
	return openClock(spec), nil
}

// validateOpenSpec refuses the malformed shapes an open request can take.
// One shape per branch, single operator each: every refusal names itself.
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

// validateChallenged completes the checks specific to the challenge flavor.
func validateChallenged(spec DuelOpenSpec) error {
	if spec.Challenged == "" {
		return fmt.Errorf("duel: challenge without a challenged party")
	}
	if spec.Opener == spec.Challenged {
		return fmt.Errorf("duel: self-challenge (%q)", spec.Opener)
	}
	return nil
}

// validateKind completes the checks for the pot flavor and rejects unknown
// kinds outright.
func validateKind(spec DuelOpenSpec) error {
	switch spec.Kind {
	case DuelPot:
		return nil
	case DuelChallenge:
		return nil // handled by validateChallenged's caller
	default:
		return fmt.Errorf("duel: unknown kind %q", spec.Kind)
	}
}

// openClock resolves which of the two windows this flavor arms.
func openClock(spec DuelOpenSpec) int64 {
	if spec.Kind == DuelChallenge {
		return ClampDuelSeconds(spec.ChallengeSeconds, DuelDefaultChallengeSeconds)
	}
	return ClampDuelSeconds(spec.PotSeconds, DuelDefaultPotSeconds)
}

// install escrows the opener and writes the duel record beside the freshly
// claimed slot. Any failure compensates by releasing the slot and refunding,
// so a half-applied open never locks the channel out of dueling.
func (s *ValkeyDuelStore) install(ctx context.Context, broadcasterID uint64, spec DuelOpenSpec) (DuelOpenResult, error) {
	res := DuelOpenResult{}
	state := DuelState{
		Kind: spec.Kind, Opener: spec.Opener, OpenerStake: spec.Stake,
		Challenged: spec.Challenged, OpenedAt: time.Now().UnixMilli(),
	}

	found, spent, err := s.cfg.Wallet.Debit(ctx, broadcasterID, spec.Opener, spec.Stake)
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

// writeInstall lands the state record and — for a pot — seeds the ledger
// with the opener: the pool is the single source of truth for the pick, the
// refunds and the digest alike. One command sets that field and the ledger's
// safety TTL together.
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

// compensateOpen rolls a failed install back: refund the opener, free the
// slot, drop any partial ledger. Best effort — a leaked deadline key only
// blocks duels until its own expiry.
func (s *ValkeyDuelStore) compensateOpen(ctx context.Context, broadcasterID uint64, state DuelState) {
	if err := s.cfg.Wallet.Credit(ctx, broadcasterID, state.Opener, state.OpenerStake); err != nil {
		s.log.Warn("duel: open rollback refund failed", zap.Uint64("broadcaster_id", broadcasterID),
			zap.String("login", state.Opener), zap.Int64("amount", state.OpenerStake), zap.Error(err))
	}
	s.releaseSlot(ctx, broadcasterID)
	s.client.Do(ctx, s.client.B().Del().
		Key(duelKey(duelEntriesPrefix, broadcasterID)).
		Key(duelKey(duelStatePrefix, broadcasterID)).Build())
}

// releaseSlot frees the channel's duel slot (open-time compensation).
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
		return res, nil // nothing running (or resolving right now)
	}
	res.Open = true
	if st.Kind != DuelPot {
		// A challenge holds the slot: joins are meaningless against it, and
		// the ledger must stay challenge-free.
		res.ChallengePending = true
		return res, nil
	}
	return s.joinPot(ctx, broadcasterID, login, stake, res)
}

// joinPot runs the money path of a pot join under the caller's section lock:
// claim the ledger seat before debiting so a duplicate join loses the race
// for the field and never moves points twice; any refusal undoes the seat so
// the ledger stays exactly the set of escrowed stakes.
func (s *ValkeyDuelStore) joinPot(ctx context.Context, broadcasterID uint64, login string, stake int64, res DuelJoinResult) (DuelJoinResult, error) {
	added, err := s.claimSeat(ctx, broadcasterID, login, stake)
	if err != nil {
		return res, err
	}
	if !added {
		res.Already = true
		return s.joinTotals(ctx, broadcasterID, res)
	}

	found, spent, err := s.cfg.Wallet.Debit(ctx, broadcasterID, login, stake)
	switch {
	case err != nil:
		s.unclaimSeat(ctx, broadcasterID, login)
		return res, err
	case !found:
		s.unclaimSeat(ctx, broadcasterID, login)
		res.Unknown = true
		return s.joinTotals(ctx, broadcasterID, res)
	case !spent:
		s.unclaimSeat(ctx, broadcasterID, login)
		res.Short = true
		return s.joinTotals(ctx, broadcasterID, res)
	}

	// Active use re-arms the safety expiry (never the deadline — that is the
	// window itself).
	s.client.Do(ctx, s.client.B().Expire().
		Key(duelKey(duelStatePrefix, broadcasterID)).Seconds(int64(duelStateTTL.Seconds())).Build())

	res.Joined = true
	return s.joinTotals(ctx, broadcasterID, res)
}

// claimSeat reserves login's ledger slot for its stake; false means the seat
// was already taken.
func (s *ValkeyDuelStore) claimSeat(ctx context.Context, broadcasterID uint64, login string, stake int64) (bool, error) {
	added, err := s.client.Do(ctx, s.client.B().Hsetnx().
		Key(duelKey(duelEntriesPrefix, broadcasterID)).
		Field(login).Value(strconv.FormatInt(stake, 10)).Build()).AsInt64()
	return added > 0, err
}

// unclaimSeat rolls a refused debit back out of the ledger so a later
// top-up can take the seat.
func (s *ValkeyDuelStore) unclaimSeat(ctx context.Context, broadcasterID uint64, login string) {
	if _, err := s.hdelEntry(ctx, broadcasterID, login); err != nil {
		s.log.Warn("duel: join rollback unclaim failed", zap.Uint64("broadcaster_id", broadcasterID),
			zap.String("login", login), zap.Error(err))
	}
}

// joinTotals fills the post-join pool readout shared by every outcome.
func (s *ValkeyDuelStore) joinTotals(ctx context.Context, broadcasterID uint64, res DuelJoinResult) (DuelJoinResult, error) {
	n, pot, err := s.readLedger(ctx, broadcasterID)
	if err != nil {
		return res, err
	}
	res.Entrants = n
	res.Pot = pot
	return res, nil
}

// readLedger sums the escrow ledger: count and total in one pass.
func (s *ValkeyDuelStore) readLedger(ctx context.Context, broadcasterID uint64) (int64, int64, error) {
	vals, err := s.client.Do(ctx, s.client.B().Hvals().
		Key(duelKey(duelEntriesPrefix, broadcasterID)).Build()).AsStrSlice()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	return int64(len(vals)), sumStakes(vals), nil
}

// sumStakes totals the readable stakes; an unreadable entry is skipped
// rather than poisoning the pot.
func sumStakes(vals []string) int64 {
	var total int64
	for _, v := range vals {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			total += n
		}
	}
	return total
}

func (s *ValkeyDuelStore) hdelEntry(ctx context.Context, broadcasterID uint64, login string) (int64, error) {
	return s.client.Do(ctx, s.client.B().Hdel().
		Key(duelKey(duelEntriesPrefix, broadcasterID)).Field(login).Build()).AsInt64()
}

// stateOf reads and decodes the live duel record under the caller's lock.
// nil means nothing runs — including an unreadable record, which logs and
// reads as closed rather than surfacing a garbage duel (the state key's own
// safety TTL retires it soon enough).
func (s *ValkeyDuelStore) stateOf(ctx context.Context, broadcasterID uint64) *DuelState {
	raw, err := s.client.Do(ctx, s.client.B().Get().
		Key(duelKey(duelStatePrefix, broadcasterID)).Build()).ToString()
	if err != nil {
		if !valkey.IsValkeyNil(err) {
			s.log.Warn("duel: state read failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		}
		return nil
	}
	st := DuelState{}
	if codec.UnmarshalFromString(raw, &st) != nil {
		s.log.Warn("duel: unreadable state", zap.Uint64("broadcaster_id", broadcasterID))
		return nil
	}
	return &st
}

// beginResolution takes the channel's resolution section and loads the live
// duel under it. busy=true means another money path holds the lock (no lock
// was taken); otherwise the caller owns the section and must releaseSection
// when done — double releases are harmless DELs. A nil state with busy=false
// means nothing runs.
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

	found, spent, err := s.cfg.Wallet.Debit(ctx, broadcasterID, login, st.OpenerStake)
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

	receipt := s.settleChallenge(ctx, broadcasterID, st)

	res.Accepted = true
	res.Winner, res.Loser, res.Pot, res.Stake = receipt.Winner, receipt.Loser, receipt.Pot, st.OpenerStake
	return res, nil
}

// settleChallenge flips the coin, tears the duel down with its receipt and
// pays the whole pot to the winner. Paying after the receipt lands leaves
// auditable evidence of an unpaid pot should a crash strike between.
func (s *ValkeyDuelStore) settleChallenge(ctx context.Context, broadcasterID uint64, st *DuelState) DuelReceipt {
	winner, loser, pot := st.Challenged, st.Opener, st.OpenerStake*2
	if FlipDuelCoin() {
		winner, loser = st.Opener, st.Challenged
	}
	receipt := DuelReceipt{
		Outcome: DuelWon, Winner: winner, Loser: loser, Pot: pot,
		ResolvedAt: time.Now().UnixMilli(),
	}
	s.teardown(ctx, broadcasterID, &receipt)
	s.payWinner(ctx, broadcasterID, &receipt)
	return receipt
}

// payWinner credits the pot recorded on a won receipt.
func (s *ValkeyDuelStore) payWinner(ctx context.Context, broadcasterID uint64, receipt *DuelReceipt) {
	if err := s.cfg.Wallet.Credit(ctx, broadcasterID, receipt.Winner, receipt.Pot); err != nil {
		s.log.Warn("duel: winner credit failed", zap.Uint64("broadcaster_id", broadcasterID),
			zap.String("winner", receipt.Winner), zap.Int64("pot", receipt.Pot), zap.Error(err))
	}
}

// challengeAddressed reports whether st is a live challenge naming login as
// its answering party — the one gate both accept and decline share.
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
		Outcome: DuelDeclined, Loser: st.Challenged, Pot: refund,
		ResolvedAt: time.Now().UnixMilli(),
	}
	s.teardown(ctx, broadcasterID, &receipt)
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

	stakes := s.escrowedStakes(ctx, broadcasterID, st)
	refunded, total := s.refundAll(ctx, broadcasterID, stakes)

	receipt := DuelReceipt{
		Outcome: DuelCanceled, Stakes: stakes, Pot: total,
		ResolvedAt: time.Now().UnixMilli(),
	}
	s.teardown(ctx, broadcasterID, &receipt)

	res.Cancelled = true
	res.Refunded = refunded
	res.Total = total
	return res, nil
}

// escrowedStakes collects what the duel owes back: for a challenge that is
// the opener's stake alone (the ledger is empty), for a pot every escrowed
// entry read fresh under the caller's section lock.
func (s *ValkeyDuelStore) escrowedStakes(ctx context.Context, broadcasterID uint64, st *DuelState) map[string]int64 {
	stakes := map[string]int64{st.Opener: st.OpenerStake}
	if st.Kind != DuelPot {
		return stakes
	}
	m, err := s.client.Do(ctx, s.client.B().Hgetall().
		Key(duelKey(duelEntriesPrefix, broadcasterID)).Build()).AsStrMap()
	if readable(err, m) {
		return stakes
	}
	return ledgerMap(parseDuelLedger(m))
}

// refundAll pays every escrowed entry back, counting only the refunds that
// actually landed.
func (s *ValkeyDuelStore) refundAll(ctx context.Context, broadcasterID uint64, stakes map[string]int64) (refunded, total int64) {
	entries := make([]DuelStake, 0, len(stakes))
	for login, amount := range stakes {
		entries = append(entries, DuelStake{Login: login, Stake: amount})
	}
	for _, entry := range SortDuelStakes(entries) {
		total += entry.Stake
		if err := s.cfg.Wallet.Credit(ctx, broadcasterID, entry.Login, entry.Stake); err != nil {
			s.log.Warn("duel: cancel refund failed", zap.Uint64("broadcaster_id", broadcasterID),
				zap.String("login", entry.Login), zap.Int64("amount", entry.Stake), zap.Error(err))
			continue
		}
		refunded++
	}
	return refunded, total
}

// refund returns one escrowed entry to its owner, logging loudly on failure.
func (s *ValkeyDuelStore) refund(ctx context.Context, broadcasterID uint64, entry DuelStake) {
	if err := s.cfg.Wallet.Credit(ctx, broadcasterID, entry.Login, entry.Stake); err != nil {
		s.log.Warn("duel: refund failed", zap.Uint64("broadcaster_id", broadcasterID),
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
		// Unreadable state: report closed rather than a garbage duel; the
		// state's own safety TTL retires the key soon enough.
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
	n, pot, err := s.readLedger(ctx, broadcasterID)
	if err != nil {
		return st, err
	}
	st.Entrants = n
	st.Pot = pot
	return st, nil
}

// teardown writes the receipt and clears the live keys. Called under the
// section lock; the entries ledger is renamed aside first so the dispute
// snapshot survives whatever wrote it.
func (s *ValkeyDuelStore) teardown(ctx context.Context, broadcasterID uint64, receipt *DuelReceipt) {
	now := receipt.ResolvedAt
	ttl := int64(duelReceiptTTL.Seconds())
	lastKey := duelKey(duelLastPrefix, broadcasterID)

	blob, err := codec.Marshal(receipt)
	if err != nil {
		// Receipt marshal of ints and strings cannot fail; if it somehow does,
		// clearing the live keys still matters more than the receipt.
		s.log.Warn("duel: receipt marshal failed", zap.Error(err))
	}
	batch := []valkey.Completed{
		s.client.B().Set().Key(lastKey).Value(string(blob)).ExSeconds(ttl).Build(),
		s.client.B().Del().Key(duelKey(duelStatePrefix, broadcasterID)).
			Key(duelKey(duelDeadlinePrefix, broadcasterID)).Build(),
	}
	// Only rename the ledger when this resolve did not already consume it
	// (the auto-draw snapshots before calling teardown).
	if receipt.SnapKey == "" {
		snap := duelSnapPrefix + strconv.FormatUint(broadcasterID, 10) + ":" + strconv.FormatInt(now, 10)
		batch = append(batch,
			s.client.B().Rename().Key(duelKey(duelEntriesPrefix, broadcasterID)).Newkey(snap).Build(),
			s.client.B().Expire().Key(snap).Seconds(ttl).Build())
	}
	for _, r := range s.client.DoMulti(ctx, batch...) {
		if err := r.Error(); !benign(err) {
			s.log.Warn("duel: teardown incomplete", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
			break
		}
	}
}

// StartExpiryWatcher implements the store's one clock off the shared expired-
// keys firehose: expired duel:deadline:<id> keys auto-resolve their duel.
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

// onExpired filters the firehose down to the duel schedule family, claims the
// expiry so exactly one replica acts, then resolves off the caller's
// goroutine.
func (s *ValkeyDuelStore) onExpired(ctx context.Context, key string) {
	if !strings.HasPrefix(key, duelDeadlinePrefix) {
		return
	}
	id, ok := parseRaffleID(strings.TrimPrefix(key, duelDeadlinePrefix)) // same non-zero-id rule
	if !ok {
		return
	}
	if s.claimExpiry(ctx, duelKey(duelClaimPrefix, id)) {
		go s.autoResolve(context.WithoutCancel(ctx), id)
	}
}

func (s *ValkeyDuelStore) claimExpiry(ctx context.Context, key string) bool {
	got, err := s.client.Do(ctx, s.client.B().Set().
		Key(key).Value("1").
		Nx().ExSeconds(int64(duelClaimTTL.Seconds())).Build()).ToString()
	return err == nil && got == "OK"
}

// autoResolve closes the expired duel: pots draw stake-weighted, challenges
// refund the stood-up opener. Losing the section lock means a join or manual
// resolve is mid-flight holding it — the expiry event behind us is already
// consumed, so we re-arm a short deadline to get another shot instead of
// stranding the duel until its state TTL.
func (s *ValkeyDuelStore) autoResolve(ctx context.Context, broadcasterID uint64) {
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if !s.holdSection(dctx, broadcasterID) {
		s.client.Do(dctx, s.client.B().Set().
			Key(duelKey(duelDeadlinePrefix, broadcasterID)).Value("retry").ExSeconds(2).Build())
		return
	}
	defer s.releaseSection(dctx, broadcasterID)

	st := s.stateOf(dctx, broadcasterID)
	if st == nil {
		return // a manual resolve beat the expiry and tore the duel down
	}
	switch st.Kind {
	case DuelChallenge:
		s.autoNoShow(dctx, broadcasterID, st)
	default:
		s.autoDraw(dctx, broadcasterID, st)
	}
}

// parseDuelLedger converts a raw hash read into canonical stakes, dropping
// unreadable or non-positive entries rather than poisoning the pool.
func parseDuelLedger(m map[string]string) []DuelStake {
	entries := make([]DuelStake, 0, len(m))
	for login, v := range m {
		n, err := strconv.ParseInt(v, 10, 64)
		if validStake(n, err) {
			entries = append(entries, DuelStake{Login: login, Stake: n})
		}
	}
	return SortDuelStakes(entries)
}

// autoDraw pays the pot to one stake-weighted winner and announces.
func (s *ValkeyDuelStore) autoDraw(ctx context.Context, broadcasterID uint64, st *DuelState) {
	m, err := s.client.Do(ctx, s.client.B().Hgetall().
		Key(duelKey(duelEntriesPrefix, broadcasterID)).Build()).AsStrMap()
	if err != nil {
		s.log.Warn("duel: auto-draw ledger read failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	sorted := parseDuelLedger(m)
	if len(sorted) == 0 {
		// Defensive: the opener seeds the ledger at open, so an empty pool
		// means corruption — refund what the state knows about rather than
		// silently vaporizing points.
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
	s.teardownWithSnapshot(ctx, broadcasterID, &receipt, snap)

	if err := s.cfg.Wallet.Credit(ctx, broadcasterID, winner, total); err != nil {
		s.log.Warn("duel: pot payout failed", zap.Uint64("broadcaster_id", broadcasterID),
			zap.String("winner", winner), zap.Int64("pot", total), zap.Error(err))
	}
	s.announce(ctx, broadcasterID, func(locale string) string {
		return expandTokens(i18nT(locale, "duel.auto_won"),
			"user", winner, "amount", strconv.FormatInt(total, 10))
	})
}

// autoNoShow refunds a challenge the named party never accepted.
func (s *ValkeyDuelStore) autoNoShow(ctx context.Context, broadcasterID uint64, st *DuelState) {
	receipt := DuelReceipt{
		Outcome: DuelNoShow, Loser: st.Challenged, Pot: st.OpenerStake,
		ResolvedAt: time.Now().UnixMilli(),
	}
	s.teardown(ctx, broadcasterID, &receipt)
	s.refund(ctx, broadcasterID, DuelStake{Login: st.Opener, Stake: st.OpenerStake})
	s.announce(ctx, broadcasterID, func(locale string) string {
		return expandTokens(i18nT(locale, "duel.auto_noshow"),
			"opener", st.Opener, "target", st.Challenged,
			"amount", strconv.FormatInt(st.OpenerStake, 10))
	})
}

// refundOnly tears a corrupted duel down with the one refund the state can
// still vouch for (defensive path; see autoDraw).
func (s *ValkeyDuelStore) refundOnly(ctx context.Context, broadcasterID uint64, st *DuelState, outcome DuelOutcome) {
	receipt := DuelReceipt{Outcome: outcome, Pot: st.OpenerStake, ResolvedAt: time.Now().UnixMilli()}
	s.teardown(ctx, broadcasterID, &receipt)
	s.refund(ctx, broadcasterID, DuelStake{Login: st.Opener, Stake: st.OpenerStake})
}

// teardownWithSnapshot is teardown for the auto-draw path, which renames the
// ledger aside itself (before crediting) so the snapshot binds to the exact
// pool the pick ran over.
func (s *ValkeyDuelStore) teardownWithSnapshot(ctx context.Context, broadcasterID uint64, receipt *DuelReceipt, snap string) {
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
			s.log.Warn("duel: snapshot teardown incomplete", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
			break
		}
	}
}

func ledgerMap(sorted []DuelStake) map[string]int64 {
	m := make(map[string]int64, len(sorted))
	for _, e := range sorted {
		m[e.Login] = e.Stake
	}
	return m
}
