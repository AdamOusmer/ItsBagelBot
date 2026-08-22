// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
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

// Raffle keyspace, one active raffle per broadcaster:
//
//	raffle:deadline:<id>  the clock — a key EX'd to the raffle's duration whose
//	                      expiry IS the auto-close (the timer: idiom). Its NX
//	                      SET at open also enforces one-active-per-channel.
//	raffle:state:<id>     the raffle record (opener, winner count), JSON.
//	raffle:entries:<id>   the entrant pool — a sorted set of user ids scored by
//	                      join time (unix millis), so ZADD NX is an atomic
//	                      enter-once-keep-nothing (a raffle has no spots).
//	raffle:claim:<id>     guards one expiry so only one replica auto-closes it
//	                      (nested under no parent on purpose: deadline expiry is
//	                      the only notification this store watches).
//	raffle:draw:<id>      the draw lock (SET NX PX), serializing the manual and
//	                      expiry draw paths against each other.
//	raffle:snap:<id>:<ts> the entrant pool renamed aside at draw time — the exact
//	                      set that produced the winners survives for the dispute
//	                      window instead of vanishing with the raffle.
//	raffle:last:<id>      the receipt hash: winners, entrant count, pool digest,
//	                      drawn-at, claims. !winner reads it; !claim writes it.
//	raffle:remind:<id>    the reminder clock — a key EX'd to the reminder
//	                      interval; each expiry posts a "minutes left" line and
//	                      re-arms itself until the deadline key is gone.
//	raffle:rclaim:<id>    guards one reminder expiry (the deadline path's own
//	                      claim is raffle:claim:<id>; separate keys so a
//	                      reminder firing next to a draw can't mute either).
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
	// raffleClaimTTL matches timerClaimTTL: long enough to cover the draw that
	// follows, short enough that a crashed claimant only mutes one auto-close.
	raffleClaimTTL = 5 * time.Second
	// raffleDrawLockTTL bounds the two-pipeline draw. A claimant that dies
	// mid-draw releases the channel after this; the worst case is a second
	// draw finding state already deleted and reporting nothing-to-draw.
	raffleDrawLockTTL = 10 * time.Second
	// raffleStateTTL reclaims a raffle abandoned without a close (crash between
	// open and any activity). Re-armed on every join, so an actively entered
	// raffle never expires mid-stream.
	raffleStateTTL = 12 * time.Hour
	// raffleReceiptTTL bounds how long the receipt and the entrant snapshot
	// survive after a draw. This is the dispute window: past it, the evidence
	// is gone and the raffle is forgotten (no database by design).
	raffleReceiptTTL = 24 * time.Hour
	// raffleClaimWindow bounds how long a winner has to !claim their prize
	// after the draw. Long enough to notice chat while still live; short
	// enough that !winner's confirmed/unclaimed split stays meaningful.
	raffleClaimWindow = 15 * time.Minute
)

// Limits clamped at Open so a hand-crafted RPC cannot arm a zero-second raffle
// or promise more winners than chat can reasonably name in one line.
const (
	minRaffleDuration    = time.Minute
	maxRaffleDuration    = 2 * time.Hour
	maxRaffleWinners     = 20
	raffleDefaultWinners = 1
	// Reminder cadence defaults to 5 minutes when open doesn't ask for one;
	// floored at a minute so a hand-crafted state cannot arm an expire/post/
	// re-arm wall of chat. Explicitly disabling is passing 0.
	raffleDefaultRemind = int64(5 * time.Minute / time.Second)
	minRaffleRemind     = int64(time.Minute / time.Second)
)

// RaffleState is the live raffle's record. Kept deliberately tiny: everything
// else derivable (entry count, time left) reads off its sibling keys.
type RaffleState struct {
	OpenedBy      string `json:"opened_by"` // the opening mod's login
	OpenedAt      int64  `json:"opened_at"` // unix millis
	Winners       int64  `json:"winners"`   // how many to draw at close
	RemindSeconds int64  `json:"remind_s"`  // reminder cadence; 0 disables
}

// RaffleResult is one completed draw. Winners are distinct user ids in draw
// order; Digest binds them to the exact entrant pool (see DigestPool). Claims
// fills in as winners confirm with !claim inside the claim window — it lives
// in a sibling hash field so the draw itself never rewrites the receipt.
type RaffleResult struct {
	Winners  []string `json:"winners"`
	Entrants int64    `json:"entrants"`
	Digest   string   `json:"digest"`
	DrawnAt  int64    `json:"drawn_at"` // unix millis
	Claims   []string `json:"-"`        // from the claims field, not result JSON
}

// RaffleOpenSpec is one Open request. Zero values mean defaults (one winner,
// ten... no — the module owns command-level defaults; the store clamps only).
// Remind below zero disables the reminder ticker, exactly zero selects the
// default cadence, anything positive is honored up to the spam floor.
type RaffleOpenSpec struct {
	OpenedBy string
	Winners  int64 // <=0 -> raffleDefaultWinners
	Duration time.Duration
	Remind   time.Duration
}

// RaffleEntry reports one !join against the live raffle: whether the caller
// got in, whether a raffle was open at all, and the pool size afterwards.
type RaffleEntry struct {
	Joined   bool
	Open     bool
	Entrants int64
}

// RaffleStatus is the live view a bare !raffle renders. SecondsLeft mirrors
// TTL semantics: -1 when no raffle runs.
type RaffleStatus struct {
	Open        bool
	Entrants    int64
	SecondsLeft int64
}

// RaffleStore is the per-broadcaster raffle surface behind the raffle module:
// one active raffle, joined with !join, closed by a mod or by its own deadline.
type RaffleStore interface {
	// Open starts a raffle lasting spec.Duration that will draw spec.Winners
	// count at close, posting a time-left reminder every spec.Remind interval.
	// ok=false means a raffle is already running (the deadline gate won).
	Open(ctx context.Context, broadcasterID uint64, spec RaffleOpenSpec) (ok bool, err error)
	// Join enters userID if a raffle is open. Open=false means no raffle is
	// running; Joined=false with Open=true means already entered.
	Join(ctx context.Context, broadcasterID uint64, userID string) (RaffleEntry, error)
	// Status reports whether a raffle is running, its pool size and the
	// seconds left on its deadline.
	Status(ctx context.Context, broadcasterID uint64) (RaffleStatus, error)
	// Draw closes the raffle and picks min(winners, entries) distinct members
	// uniformly at random. nil result means no raffle was running; a non-nil
	// result with no winners means it closed with an empty pool. The receipt
	// and entrant snapshot outlive the call for the dispute window.
	Draw(ctx context.Context, broadcasterID uint64, winners int64) (*RaffleResult, error)
	// Cancel tears the raffle down without drawing. ok=false when none ran.
	Cancel(ctx context.Context, broadcasterID uint64) (ok bool, err error)
	// LastResult recalls the most recent draw's receipt for !winner, claims
	// included.
	LastResult(ctx context.Context, broadcasterID uint64) (*RaffleResult, bool, error)
	// Claim lets a winner of the most recent draw confirm their prize inside
	// the claim window. The outcome says which reply fits; ClaimNone covers
	// both "no recent draw" and "not among the winners" — chat has no business
	// distinguishing them.
	Claim(ctx context.Context, broadcasterID uint64, userID string) (RaffleClaim, error)
	// StartExpiryWatcher subscribes to Valkey key-expiry notifications,
	// auto-closes (draws) each raffle whose deadline key expires, and posts
	// each reminder tick. It runs until ctx is cancelled and reconnects on a
	// dropped subscription. Requires notify-keyspace-events to include
	// expired-key events (Ex), already on for the live/timer/loyalty watchers
	// this shares the deployment with.
	StartExpiryWatcher(ctx context.Context)
}

// RaffleConfig carries everything the store needs beyond the Valkey client:
// where announcements go (the premium/standard lane split) and who resolves a
// broadcaster's tier and locale on those paths. Key expiry has no invoking
// chat message, so — like the timer store firing its configured message — this
// store posts its lines itself.
type RaffleConfig struct {
	OutgressPremiumSubject  string
	OutgressStandardSubject string
	Pub                     bus.Publisher
	Proj                    projection.Reader
}

// ValkeyRaffleStore implements RaffleStore on the shared Valkey client. Like
// the queue store it runs on a primary-consistent view: one broadcaster's chat
// drives the whole raffle in sequence, so a node-local replica serving Status
// right after Open would tell chat the raffle doesn't exist yet.
type ValkeyRaffleStore struct {
	client valkey.Client
	cfg    RaffleConfig
	log    *zap.Logger

	rng func(total, n int) []int // partial Fisher-Yates over indices; swappable in tests
}

// NewValkeyRaffleStore builds the store on a primary-consistent view.
func NewValkeyRaffleStore(client valkey.Client, cfg RaffleConfig, log *zap.Logger) *ValkeyRaffleStore {
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyRaffleStore{client: pkg_valkey.Primary(client), cfg: cfg, log: log, rng: rngPick}
}

// Pure, so the gate below stays a straight line; remindSecs is what the
// reminder clock arms with (0: no reminders).

// armDeadline claims the channel's raffle slot: exactly one caller's SET NX
// wins, everyone else reports already-open. One round trip, correct across
// replicas — the cooldown idiom doubling as a clock. ok=false means the slot
// was taken.
func (s *ValkeyRaffleStore) armDeadline(ctx context.Context, broadcasterID uint64, duration time.Duration) (bool, error) {
	got, err := s.client.Do(ctx, s.client.B().Set().
		Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).
		Value("1").
		Nx().ExSeconds(int64(duration.Seconds())).Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil // deadline still held: a raffle is already open
		}
		return false, err
	}
	return got == "OK", nil
}

// installState writes the raffle record beside a freshly claimed deadline,
// clears any leftover entrant pool and arms the reminder clock. A failure here
// compensates by releasing the deadline, so a half-applied open never locks
// the channel out of opening a raffle.
func (s *ValkeyRaffleStore) installState(ctx context.Context, broadcasterID uint64, state []byte, remindSecs int64) error {
	batch := []valkey.Completed{
		s.client.B().Set().Key(raffleKey(raffleStatePrefix, broadcasterID)).
			Value(string(state)).ExSeconds(int64(raffleStateTTL.Seconds())).Build(),
		s.client.B().Del().Key(raffleKey(raffleEntriesPrefix, broadcasterID)).Build(),
	}
	// The reminder clock: armed once here, re-armed by its own expiry path
	// until the deadline key disappears. Leftovers from a prior raffle are
	// overwritten either way.
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
	// One round trip: prove the raffle is open (state exists — the deadline key
	// alone can't distinguish open from seconds-before-auto-close), claim the
	// entry (NX keeps an existing one), read the pool size, re-arm both safety
	// expiries. Joins prove active use, like the queue's re-arm.
	//
	// TOCTOU note: a raffle closing between EXISTS and ZADD leaves one stray
	// entry in a pool about to be renamed aside — it joins a snapshot it can't
	// win from. Harmless by construction; not worth a Lua round trip.
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

// decodeReceipt parses one receipt's stored fields: the result blob is
// authoritative, the claims blob best-effort — a corrupt claim array must not
// hide the winners. ok=false when there is no receipt or the result blob is
// unreadable.
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

// holdDraw takes the per-channel draw lock so the manual command path and the
// deadline expiry cannot interleave their read/mutate phases. false means
// another draw holds it and will announce; callers stay silent.
func (s *ValkeyRaffleStore) holdDraw(ctx context.Context, broadcasterID uint64) bool {
	got, err := s.client.Do(ctx, s.client.B().Set().Key(raffleKey(raffleDrawPrefix, broadcasterID)).Value("1").
		Nx().PxMilliseconds(raffleDrawLockTTL.Milliseconds()).Build()).ToString()
	if err != nil {
		return false
	}
	return got == "OK"
}

// drawRead is one draw's inputs under the lock: the winner count (the
// override when positive, else the state's configured count) and the full
// canonical pool sorted by join time. A nil result means no raffle was
// running. ZRANGE over a raffle-sized zset is thousands of small strings at
// worst — bounded by chat, not by design.
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
			return nil, nil // nothing running
		}
		if err != nil {
			return nil, err
		}
		st := RaffleState{}
		if codec.UnmarshalFromString(v, &st) != nil {
			return read, nil // unreadable state: draw with the raw override
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

// writeDrawPhase tears the drawn raffle down and leaves the evidence: the pool
// renamed aside intact (the auditable artifact), the receipt hash written,
// state/deadline/reminder keys cleared. Between the read and this pipeline a
// join can land in the pool — such an entrant is snapshotted but was never
// eligible for the pick above; the digest makes the discrepancy visible rather
// than silent, which is the point.
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
			s.log.Warn("raffle: draw teardown incomplete", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
			break
		}
	}
}

func (s *ValkeyRaffleStore) Draw(ctx context.Context, broadcasterID uint64, winners int64) (*RaffleResult, error) {
	if !s.holdDraw(ctx, broadcasterID) {
		return nil, nil // another path holds the draw: let it announce
	}

	read, err := s.readDrawPhase(ctx, broadcasterID, winners)
	if err != nil || read == nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	res := &RaffleResult{
		Winners:  pickWinners(s.rng, read.Members, read.Count),
		Entrants: int64(len(read.Members)),
		Digest:   DigestPool(read.Members),
		DrawnAt:  now,
	}
	s.writeDrawPhase(ctx, broadcasterID, res)
	return res, nil
}

// StartExpiryWatcher implements two clocks off one subscription: expired
// raffle:deadline:<id> keys draw (the auto-close — a manual !raffle draw that
// beat the expiry deletes the key first, so this path finds nothing to do),
// and expired raffle:remind:<id> keys post the time-left line and re-arm.
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

// onExpired filters the shared expired-keys firehose down to the two raffle
// schedule key families, claims each expiry so exactly one replica acts, then
// runs the matching path off the caller's goroutine.
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

// parseRaffleID accepts only non-zero broadcaster ids; anything else riding
// the firehose with a matching prefix is noise.
func parseRaffleID(s string) (uint64, bool) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

// claimExpiry takes this replica's per-key claim so one expiry fires once
// across the fleet (the timer:claim idiom).
func (s *ValkeyRaffleStore) claimExpiry(ctx context.Context, key string) bool {
	got, err := s.client.Do(ctx, s.client.B().Set().
		Key(key).Value("1").
		Nx().ExSeconds(int64(raffleClaimTTL.Seconds())).Build()).ToString()
	return err == nil && got == "OK"
}

// and the pool's canonical form (join-time-sorted members, newline-joined).
// Anyone holding the announced winners, the entrant count and the snapshot can
// recompute it and detect a pool that changed after the fact. The snapshot key
// carries the same unix-milli stamp as DrawnAt so the pair is unambiguous.
