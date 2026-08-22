// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/internal/projection"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"ItsBagelBot/pkg/bus"
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

// RaffleStore is the per-broadcaster raffle surface behind the raffle module:
// one active raffle, joined with !join, closed by a mod or by its own deadline.
type RaffleStore interface {
	// Open starts a raffle lasting duration that will draw winners count at
	// close, posting a time-left reminder every remind interval (0: default,
	// negative: none). ok=false means a raffle is already running (the
	// deadline gate won).
	Open(ctx context.Context, broadcasterID uint64, openedBy string, winners int64, duration, remind time.Duration) (ok bool, err error)
	// Join enters userID if a raffle is open. open=false means no raffle is
	// running; joined=false with open=true means already entered. entrants
	// reports the pool size either way.
	Join(ctx context.Context, broadcasterID uint64, userID string) (joined bool, entrants int64, open bool, err error)
	// Status reports whether a raffle is running, its pool size and the
	// seconds left on its deadline (-1 when none runs, mirroring TTL semantics).
	Status(ctx context.Context, broadcasterID uint64) (open bool, entrants, secondsLeft int64, err error)
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

// RaffleClaim is the outcome of a winner's !claim against the latest receipt.
type RaffleClaim int

const (
	ClaimOk      RaffleClaim = iota // first claim by this winner, recorded
	ClaimAlready                    // this winner already confirmed
	ClaimLate                       // past raffleClaimWindow since the draw
	ClaimNone                       // no receipt, or caller isn't among the winners
)

// claimScript validates and records one !claim atomically: winner membership,
// duplicate and window checks all read and write inside the script, so two
// winners claiming in the same instant can't lose an update between HGET and
// HSET. String sentinels carry the outcome (cjson handles the result blob).
var claimScript = valkey.NewLuaScript(`
local r = redis.call('HGET', KEYS[1], 'result')
if not r then return '-none' end
local ok, res = pcall(cjson.decode, r)
if not ok then return '-none' end
local found = false
for _, w in ipairs(res.winners or {}) do
  if w == ARGV[1] then found = true end
end
if not found then return '-none' end
local claims = {}
local c = redis.call('HGET', KEYS[1], 'claims')
if c then
  local ok2, list = pcall(cjson.decode, c)
  if ok2 then claims = list end
end
for _, cl in ipairs(claims) do
  if cl == ARGV[1] then return '-already' end
end
if tonumber(ARGV[3]) > tonumber(res.drawn_at or 0) + tonumber(ARGV[2]) * 1000 then
  return '-late'
end
table.insert(claims, ARGV[1])
redis.call('HSET', KEYS[1], 'claims', cjson.encode(claims))
return '+ok'
`)

// RaffleConfig carries the announcement path the auto-close and the reminder
// ticks need: key expiry has no invoking chat message, so — like the timer
// store firing its configured message — this store posts its lines itself,
// splitting the premium/standard lanes off the broadcaster's tier.
type RaffleConfig struct {
	OutgressPremiumSubject  string
	OutgressStandardSubject string
}

// ValkeyRaffleStore implements RaffleStore on the shared Valkey client. Like
// the queue store it runs on a primary-consistent view: one broadcaster's chat
// drives the whole raffle in sequence, so a node-local replica serving Status
// right after Open would tell chat the raffle doesn't exist yet.
type ValkeyRaffleStore struct {
	client valkey.Client
	pub    bus.Publisher
	proj   projection.Reader
	cfg    RaffleConfig
	log    *zap.Logger

	rng func(total, n int) []int // partial Fisher-Yates over indices; swappable in tests
}

// NewValkeyRaffleStore builds the store on a primary-consistent view. proj
// resolves the broadcaster's tier for the auto-close lane split.
func NewValkeyRaffleStore(client valkey.Client, pub bus.Publisher, proj projection.Reader, cfg RaffleConfig, log *zap.Logger) *ValkeyRaffleStore {
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyRaffleStore{client: pkg_valkey.Primary(client), pub: pub, proj: proj, cfg: cfg, log: log, rng: rngPick}
}

func raffleKey(prefix string, id uint64) string { return prefix + strconv.FormatUint(id, 10) }

func (s *ValkeyRaffleStore) Open(ctx context.Context, broadcasterID uint64, openedBy string, winners int64, duration, remind time.Duration) (bool, error) {
	if winners <= 0 {
		winners = raffleDefaultWinners
	}
	if winners > maxRaffleWinners {
		winners = maxRaffleWinners
	}
	switch {
	case duration < minRaffleDuration:
		duration = minRaffleDuration
	case duration > maxRaffleDuration:
		duration = maxRaffleDuration
	}
	var remindSecs int64
	switch {
	case remind < 0:
		// Explicitly no reminders.
	case remind == 0:
		remindSecs = raffleDefaultRemind
	default:
		if secs := int64(remind.Seconds()); secs > minRaffleRemind {
			remindSecs = secs
		} else {
			remindSecs = minRaffleRemind
		}
	}

	// The deadline key is the whole mutual exclusion: exactly one caller's SET
	// NX wins, everyone else reports already-open. One round trip, correct
	// across replicas — the cooldown idiom doubling as a clock.
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
	if got != "OK" {
		return false, nil
	}

	state, err := json.Marshal(RaffleState{
		OpenedBy: openedBy, OpenedAt: time.Now().UnixMilli(), Winners: winners, RemindSeconds: remindSecs,
	})
	if err == nil {
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
		// Leftover entrants from a cancelled/abandoned prior raffle must not
		// leak into this one.
		for _, r := range s.client.DoMulti(ctx, batch...) {
			if err = r.Error(); err != nil && !valkey.IsValkeyNil(err) {
				break
			}
		}
	}
	if err != nil {
		// Compensate: release the gate so the channel isn't locked out of
		// opening a raffle by a half-applied one.
		_ = s.client.Do(ctx, s.client.B().Del().Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).Build()).Error()
		return false, err
	}
	return true, nil
}

func (s *ValkeyRaffleStore) Join(ctx context.Context, broadcasterID uint64, userID string) (bool, int64, bool, error) {
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
	exists, err := resps[0].AsInt64()
	if err != nil {
		return false, 0, false, err
	}
	entrants, err := resps[2].AsInt64()
	if err != nil {
		return false, 0, false, err
	}
	if exists == 0 {
		return false, entrants, false, nil
	}
	added, err := resps[1].AsInt64()
	if err != nil {
		return false, 0, true, err
	}
	return added > 0, entrants, true, nil
}

func (s *ValkeyRaffleStore) Status(ctx context.Context, broadcasterID uint64) (bool, int64, int64, error) {
	key := raffleKey(raffleEntriesPrefix, broadcasterID)
	resps := s.client.DoMulti(ctx,
		s.client.B().Exists().Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).Build(),
		s.client.B().Zcard().Key(key).Build(),
		s.client.B().Ttl().Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).Build(),
	)
	open, err := resps[0].AsInt64()
	if err != nil {
		return false, 0, -1, err
	}
	entrants, err := resps[1].AsInt64()
	if err != nil {
		return false, 0, -1, err
	}
	ttl, err := resps[2].AsInt64()
	if err != nil {
		return false, 0, -1, err
	}
	return open > 0, entrants, ttl, nil
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
	if len(m) == 0 {
		return nil, false, nil
	}
	var res RaffleResult
	if err := json.Unmarshal([]byte(m["result"]), &res); err != nil {
		return nil, false, err
	}
	if claims := m["claims"]; claims != "" {
		if json.Unmarshal([]byte(claims), &res.Claims) != nil {
			res.Claims = nil // a corrupt claim list must not hide the winners
		}
	}
	return &res, true, nil
}

func (s *ValkeyRaffleStore) Draw(ctx context.Context, broadcasterID uint64, winners int64) (*RaffleResult, error) {
	claimKey := raffleKey(raffleDrawPrefix, broadcasterID)
	got, err := s.client.Do(ctx, s.client.B().Set().Key(claimKey).Value("1").
		Nx().PxMilliseconds(raffleDrawLockTTL.Milliseconds()).Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, nil // another path holds the draw: let it announce
		}
		return nil, err
	}
	if got != "OK" {
		return nil, nil
	}

	stateKey := raffleKey(raffleStatePrefix, broadcasterID)
	poolKey := raffleKey(raffleEntriesPrefix, broadcasterID)

	// Read phase under the lock: the record (for the configured winner count
	// when the override says 0) and the full canonical pool (sorted by join
	// time) the pick and digest both need. ZRANGE over a raffle-sized zset is
	// thousands of small strings at worst — bounded by chat, not by design.
	cfgCount := winners
	var members []string
	if cfgCount <= 0 {
		v, err := s.client.Do(ctx, s.client.B().Get().Key(stateKey).Build()).ToString()
		if err != nil && !valkey.IsValkeyNil(err) {
			return nil, err
		}
		if valkey.IsValkeyNil(err) {
			return nil, nil // nothing running
		}
		st := RaffleState{}
		if err := json.Unmarshal([]byte(v), &st); err != nil {
			return nil, err
		}
		cfgCount = st.Winners
	}
	members, err = s.client.Do(ctx, s.client.B().Zrange().Key(poolKey).Min("0").Max("-1").Build()).AsStrSlice()
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	res := &RaffleResult{
		Entrants: int64(len(members)),
		Digest:   DigestPool(members),
		DrawnAt:  now,
	}
	if n := cfgCount; n < int64(len(members)) {
		pick := s.rng(len(members), int(n))
		res.Winners = make([]string, len(pick))
		for i, idx := range pick {
			res.Winners[i] = members[idx]
		}
	} else {
		res.Winners = members // fewer entrants than winners: everyone wins
	}

	// Mutate phase, still under the lock: rename the pool aside intact (the
	// auditable artifact), tear the raffle down, write the receipt. Between the
	// read and this pipeline a join can land in the pool — such an entrant is
	// snapshotted but was never eligible for the pick above; the digest makes
	// the discrepancy visible rather than silent, which is the point.
	snapKey := raffleSnapPrefix + strconv.FormatUint(broadcasterID, 10) + ":" + strconv.FormatInt(now, 10)
	resultJSON := marshalJSON(res)
	for _, r := range s.client.DoMulti(ctx,
		s.client.B().Rename().Key(poolKey).Newkey(snapKey).Build(),
		s.client.B().Expire().Key(snapKey).Seconds(int64(raffleReceiptTTL.Seconds())).Build(),
		s.client.B().Del().Key(stateKey).
			Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).
			Key(raffleKey(raffleRemindPrefix, broadcasterID)).Build(),
		s.client.B().Hset().Key(raffleKey(raffleLastPrefix, broadcasterID)).FieldValue().
			FieldValue("result", resultJSON).Build(),
		s.client.B().Expire().Key(raffleKey(raffleLastPrefix, broadcasterID)).Seconds(int64(raffleReceiptTTL.Seconds())).Build(),
	) {
		if err = r.Error(); err != nil && !valkey.IsValkeyNil(err) {
			s.log.Warn("raffle: draw teardown incomplete", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
			break
		}
	}
	return res, nil
}

// Claim records a winner's prize confirmation on the latest receipt. All
// validation rides the script; Go only translates the sentinel.
func (s *ValkeyRaffleStore) Claim(ctx context.Context, broadcasterID uint64, userID string) (RaffleClaim, error) {
	if userID == "" {
		return ClaimNone, nil
	}
	out, err := claimScript.Exec(ctx, s.client,
		[]string{raffleKey(raffleLastPrefix, broadcasterID)},
		[]string{userID,
			strconv.FormatInt(int64(raffleClaimWindow.Seconds()), 10),
			strconv.FormatInt(time.Now().UnixMilli(), 10)},
	).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			// Script replies starting with '-' are error replies in RESP2;
			// valkey-go surfaces them as errors, not strings.
			return ClaimNone, nil
		}
		return ClaimNone, err
	}
	switch out {
	case "+ok":
		return ClaimOk, nil
	case "-already":
		return ClaimAlready, nil
	case "-late":
		return ClaimLate, nil
	default: // "-none" and anything unexpected
		return ClaimNone, nil
	}
}

// rngPick returns n distinct indices uniform over [0,total) via partial
// Fisher-Yates with crypto/rand. It lives on the store as an indirection so
// tests can pin the pick while production draws stay cryptographically random.
func rngPick(total, n int) []int {
	idx := make([]int, total)
	for i := range idx {
		idx[i] = i
	}
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(total-i)))
		if err != nil {
			// CSPRNG unavailable is not survivable for a fair draw; fail loudly.
			panic("raffle: crypto/rand unavailable: " + err.Error())
		}
		k := i + int(j.Int64())
		idx[i], idx[k] = idx[k], idx[i]
		out = append(out, idx[i])
	}
	return out
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
			if s.claimExpiry(ctx, raffleClaimPrefix, id) {
				go s.autoDraw(context.WithoutCancel(ctx), id)
			}
		}
	case strings.HasPrefix(key, raffleRemindPrefix):
		idStr := strings.TrimPrefix(key, raffleRemindPrefix)
		if id, ok := parseRaffleID(idStr); ok {
			if s.claimExpiry(ctx, raffleRClaimPrefix, id) {
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
func (s *ValkeyRaffleStore) claimExpiry(ctx context.Context, prefix string, broadcasterID uint64) bool {
	got, err := s.client.Do(ctx, s.client.B().Set().
		Key(raffleKey(prefix, broadcasterID)).Value("1").
		Nx().ExSeconds(int64(raffleClaimTTL.Seconds())).Build()).ToString()
	return err == nil && got == "OK"
}

// autoDraw draws with the state's configured winner count and announces.
func (s *ValkeyRaffleStore) autoDraw(ctx context.Context, broadcasterID uint64) {
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := s.Draw(dctx, broadcasterID, 0) // 0: use the state's configured count
	if err != nil {
		s.log.Warn("raffle: auto-close draw failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	if res == nil {
		return // manual draw beat the expiry and tore the raffle down first
	}
	locale := s.localeOf(dctx, broadcasterID)
	var text string
	if len(res.Winners) == 0 {
		text = i18n.T(locale, "raffle.auto_empty")
	} else {
		text = expandTokens(i18n.T(locale, "raffle.auto_closed"), map[string]string{
			"targets":  mentionList(res.Winners),
			"count":    strconv.FormatInt(int64(len(res.Winners)), 10),
			"entrants": strconv.FormatInt(res.Entrants, 10),
			"claim":    strconv.FormatInt(int64(raffleClaimWindow.Minutes()), 10),
		})
	}
	s.post(dctx, broadcasterID, text)
}

// remindTick posts the time-left line and re-arms the reminder clock until the
// deadline key is gone (drawn or cancelled): the next expiry lands at min(
// configured interval, time actually left), so the last reminder never
// overshoots the draw. A raffle opened without reminders has no key here.
func (s *ValkeyRaffleStore) remindTick(ctx context.Context, broadcasterID uint64) {
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resps := s.client.DoMulti(dctx,
		s.client.B().Ttl().Key(raffleKey(raffleDeadlinePrefix, broadcasterID)).Build(),
		s.client.B().Zcard().Key(raffleKey(raffleEntriesPrefix, broadcasterID)).Build(),
		s.client.B().Get().Key(raffleKey(raffleStatePrefix, broadcasterID)).Build(),
	)
	left, err := resps[0].AsInt64()
	if err != nil || left <= 0 {
		return // drawn or cancelled between expiry and tick: stay quiet
	}
	st := RaffleState{}
	if v, err := resps[2].ToString(); err == nil {
		_ = json.Unmarshal([]byte(v), &st)
	}
	entrants, err := resps[1].AsInt64()
	if err != nil {
		return
	}

	locale := s.localeOf(dctx, broadcasterID)
	s.post(dctx, broadcasterID, expandTokens(i18n.T(locale, "raffle.remind"), map[string]string{
		"mins":  strconv.FormatInt((left+59)/60, 10),
		"count": strconv.FormatInt(entrants, 10),
	}))

	// Re-arm at min(interval, left): the final tick lands just before the draw.
	next := st.RemindSeconds
	if next <= 0 {
		next = raffleDefaultRemind // legacy state without a cadence field
	}
	if next > left {
		next = left
	}
	s.client.Do(dctx, s.client.B().Set().Key(raffleKey(raffleRemindPrefix, broadcasterID)).
		Value("1").ExSeconds(next).Build())
}

// localeOf resolves the broadcaster's console language for engine-side lines,
// degrading to the catalog default on any projection failure.
func (s *ValkeyRaffleStore) localeOf(ctx context.Context, broadcasterID uint64) string {
	if u, err := s.proj.User(ctx, broadcasterID); err == nil {
		return u.Locale
	}
	return ""
}

// post sends one engine-side chat line the way the timer store fires its
// message: the send-time floor guard first, then whichever premium/standard
// lane the broadcaster's own tier resolves to.
func (s *ValkeyRaffleStore) post(ctx context.Context, broadcasterID uint64, text string) {
	if text == "" {
		return
	}
	if term, hit := moderation.CheckFloor(text); hit {
		s.log.Warn("raffle: suppressed announcement carrying floor content",
			zap.Uint64("broadcaster_id", broadcasterID), zap.String("term", term))
		return
	}

	subject := s.cfg.OutgressStandardSubject
	if u, err := s.proj.User(ctx, broadcasterID); err == nil && u.Premium() {
		subject = s.cfg.OutgressPremiumSubject
	}
	body, err := buildOutgress(&module.Output{Type: outgress.TypeChat, BroadcasterID: strconv.FormatUint(broadcasterID, 10), Text: text})
	if err != nil {
		s.log.Warn("raffle: failed to build outgress message", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	if err := bus.PublishRaw(ctx, s.pub, subject, body); err != nil {
		s.log.Warn("raffle: failed to publish", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

// DigestPool is the receipt's tamper-evidence: SHA-256 over the version tag
// and the pool's canonical form (join-time-sorted members, newline-joined).
// Anyone holding the announced winners, the entrant count and the snapshot can
// recompute it and detect a pool that changed after the fact. The snapshot key
// carries the same unix-milli stamp as DrawnAt so the pair is unambiguous.
func DigestPool(members []string) string {
	h := sha256.New()
	h.Write([]byte("raffle-v1\n"))
	h.Write([]byte(strings.Join(members, "\n")))
	return hex.EncodeToString(h.Sum(nil))
}

// marshalJSON is json.Marshal ignoring the error for the one shape here (a
// slice and ints cannot fail); kept named so the call site explains itself.
func marshalJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// mentionList renders winner ids as chat mentions: "@a, @b". Winners are
// stored as logins (the queue precedent), so a prefix is all it takes.
func mentionList(winners []string) string {
	prefixed := make([]string, len(winners))
	for i, w := range winners {
		prefixed[i] = "@" + w
	}
	return strings.Join(prefixed, ", ")
}

// expandTokens substitutes {token} placeholders with values; unknown tokens
// pass through untouched.
func expandTokens(tmpl string, kv map[string]string) string {
	return strings.NewReplacer(func() []string {
		out := make([]string, 0, len(kv)*2)
		for k, v := range kv {
			out = append(out, "{"+k+"}", v)
		}
		return out
	}()...).Replace(tmpl)
}
