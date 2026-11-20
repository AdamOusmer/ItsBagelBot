// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw_test

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

type fixture struct {
	*draw.Store
	client valkey_go.Client
	t      *testing.T
}

func newFixture(t *testing.T, cfg draw.Config) fixture {
	t.Helper()
	client := valkeytest.Real(t)
	return fixture{Store: draw.NewStore(client, testConfig(cfg)), client: client, t: t}
}

type ignoreNotices struct{}

func (ignoreNotices) Announce(context.Context, draw.Notice) error { return nil }

func testConfig(cfg draw.Config) draw.Config {
	if cfg.Verifier == nil {
		cfg.Verifier = draw.PassAll{}
	}
	if cfg.Payer == nil {
		cfg.Payer = testPayer{}
	}
	if cfg.Announcer == nil {
		cfg.Announcer = ignoreNotices{}
	}
	if cfg.Winners == nil {
		cfg.Winners = draw.DiscardWinnerRecords{}
	}
	if cfg.Phases == nil {
		cfg.Phases = draw.IgnorePhaseChanges{}
	}
	return cfg
}

func (f fixture) start(s *draw.Store) {
	ctx, cancel := context.WithCancel(f.t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	f.t.Cleanup(func() {
		cancel()
		<-done
	})
}

func (f fixture) open(channel draw.ChannelID, terms draw.Terms) draw.Round {
	f.t.Helper()
	return f.openFor(channel, terms, time.Minute)
}

func (f fixture) openFor(channel draw.ChannelID, terms draw.Terms, window time.Duration) draw.Round {
	f.t.Helper()
	terms.ClosesAt = f.now().Add(window)
	return f.openRequest(draw.OpenRequest{Channel: channel, Terms: terms})
}

func (f fixture) openRequest(req draw.OpenRequest) draw.Round {
	f.t.Helper()
	r, result, err := f.Open(f.t.Context(), req)
	require.NoError(f.t, err)
	require.Equal(f.t, draw.Opened, result)
	f.t.Cleanup(func() {
		ctx := context.Background()
		keys := drawKeys(r)
		require.NoError(f.t, keys.Teardown(ctx, f.client, string(r.ID)))
	})
	return r
}

func (f fixture) join(r draw.Round, viewer draw.ViewerID) draw.JoinOutcome {
	f.t.Helper()
	result, err := f.Join(f.t.Context(), draw.JoinRequest{Channel: r.Channel, Round: r.ID,
		Viewer: viewer, Login: "viewer" + string(viewer), Weight: 100})
	require.NoError(f.t, err)
	return result
}

func (f fixture) close(channel draw.ChannelID) {
	f.t.Helper()
	closed, err := f.Close(f.t.Context(), draw.CloseRequest{Channel: channel, Actor: "mod"})
	require.NoError(f.t, err)
	require.True(f.t, closed)
}

func drawKeys(r draw.Round) valkey.RoundKeys {
	channel := strconv.FormatUint(uint64(r.Channel), 10)
	id := string(r.ID)
	return valkey.RoundKeys{Joinable: draw.JoinableKey(r.Channel), Active: "draw:active:" + channel,
		Round: "draw:round:" + id, Entries: "draw:entries:" + id, Labels: "draw:labels:" + id,
		Refused: "draw:refused:" + id, RefusalPending: "draw:refusal-pending:" + id, Slots: "draw:slots:" + id, Claimable: "draw:claimable:" + channel, Due: "draw:due", Lease: "draw:lease:" + id}
}

func TestDrawCloseAndReopen(t *testing.T) {
	f := newFixture(t, draw.Config{})
	r := f.open(401, draw.Terms{Kind: draw.KindGiveaway})
	require.Equal(t, draw.JoinOutcome{Open: true, Added: true, Count: 1}, f.join(r, "11"))
	require.Equal(t, draw.JoinOutcome{Open: true, Count: 1}, f.join(r, "11"))
	f.close(r.Channel)
	next := f.open(r.Channel, r.Terms)
	require.Equal(t, draw.JoinOutcome{Count: 1}, f.join(r, "12"))
	f.start(f.Store)
	waitRemoved(t, f.client, drawKeys(r))
	require.NoError(t, (valkey.DueIndex{Key: "draw:due"}).LapseNow(t.Context(), f.client,
		valkey.DueNotice{Member: string(r.ID), Channel: "draw:due:wake"}))
	waitRemoved(t, f.client, drawKeys(r))
	got, found, err := f.Joinable(t.Context(), next.Channel)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, next.ID, got.ID)
}

func waitRemoved(t *testing.T, client valkey_go.Client, keys valkey.RoundKeys) {
	t.Helper()
	id := keys.Round[len("draw:round:"):]
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		raw, err := keys.Metadata(t.Context(), client)
		require.NoError(check, err)
		require.Empty(check, raw)
		ids, err := valkey.ActiveRounds(t.Context(), client, keys.Due)
		require.NoError(check, err)
		require.NotContains(check, ids, id)
	}, 8*time.Second, 10*time.Millisecond)
}

func TestDrawConcurrentClose(t *testing.T) {
	f := newFixture(t, draw.Config{})
	r := f.open(402, draw.Terms{Kind: draw.KindGiveaway})
	type outcome struct {
		applied bool
		err     error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			ok, err := f.Close(t.Context(), draw.CloseRequest{Channel: r.Channel})
			results <- outcome{ok, err}
		}()
	}
	close(start)
	var wins int
	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		if result.applied {
			wins++
		}
	}
	require.Equal(t, 1, wins)
}

type testPayer struct {
	paid        chan draw.Payment
	err         error
	instanceErr error
}

func (p testPayer) Pay(_ context.Context, payment draw.Payment) error {
	if p.paid != nil {
		p.paid <- payment
	}
	return p.err
}

func (p testPayer) Instance(context.Context, draw.ChannelID, draw.ViewerID) (int64, error) {
	return 901, p.instanceErr
}

func TestDrawResumesPayoutFromDueIndex(t *testing.T) {
	payer := testPayer{paid: make(chan draw.Payment, 5)}
	f := newFixture(t, draw.Config{})
	r := f.open(403, draw.Terms{Kind: draw.KindRaffle, Prize: draw.Prize{Points: 17}, Winners: draw.WinnerPolicy{Count: 2}})
	f.join(r, "11")
	f.join(r, "12")
	f.close(r.Channel)
	f.start(draw.NewStore(f.client, testConfig(draw.Config{Payer: payer, Random: func(int64) int64 { return 0 }})))
	got := []draw.Payment{take(t, payer.paid), take(t, payer.paid)}
	slices.SortFunc(got, func(a, b draw.Payment) int { return cmp.Compare(a.Viewer, b.Viewer) })
	for n, viewer := range []draw.ViewerID{"11", "12"} {
		want := draw.Payment{Channel: r.Channel, Viewer: viewer, Login: "viewer" + string(viewer),
			Key: draw.PaymentKey(fmt.Sprintf("raffle:%s:%d", r.ID, n+1)), Points: 8, Instance: 901}
		require.Equal(t, want, got[n])
	}
	waitRemoved(t, f.client, drawKeys(r))
	require.Empty(t, payer.paid)
}

func take[T any](t *testing.T, events <-chan T) T {
	t.Helper()
	select {
	case got := <-events:
		return got
	case <-time.After(8 * time.Second):
		t.Fatal("draw work did not complete")
		var zero T
		return zero
	}
}

type notices chan draw.Notice

func (n notices) Announce(_ context.Context, notice draw.Notice) error {
	n <- notice
	return nil
}

func (n notices) next(t *testing.T, event draw.NoticeEvent) draw.Notice {
	t.Helper()
	for {
		if notice := take(t, n); notice.Event == event {
			return notice
		}
	}
}

func TestDrawRerollUsesFreshClaimWindowAndGeneration(t *testing.T) {
	notice := make(notices, 10)
	f := newFixture(t, draw.Config{Announcer: notice, Random: func(int64) int64 { return 0 }})
	r := f.open(404, draw.Terms{Kind: draw.KindGiveaway, ClaimWindow: 30 * time.Second, Winners: draw.WinnerPolicy{Count: 2}})
	for _, viewer := range []draw.ViewerID{"11", "12", "13", "14"} {
		f.join(r, viewer)
	}
	f.close(r.Channel)
	f.start(f.Store)
	first := notice.next(t, draw.NoticeWinners).Slots
	require.Len(t, first, 2)
	require.Equal(t, first[0].Deadline, first[1].Deadline)
	f.expireLine(r, first)
	require.Len(t, notice.next(t, draw.NoticeNoShow).Slots, 2)
	rerolled, err := f.Reroll(t.Context(), draw.SlotAction{Channel: r.Channel, Actor: "mod"})
	require.NoError(t, err)
	require.Equal(t, 2, rerolled)
	second := notice.next(t, draw.NoticeWinners).Slots
	verifyReplacement(t, first, second)
	f.claimOldGeneration(r, second[0])
}

func verifyReplacement(t *testing.T, first, second []draw.Slot) {
	t.Helper()
	require.Len(t, second, 2)
	require.Equal(t, second[0].Deadline, second[1].Deadline)
	for i, got := range second {
		require.Equal(t, draw.ViewerID(strconv.Itoa(13+i)), got.Viewer)
		require.EqualValues(t, 2, got.Generation)
		require.True(t, got.Deadline.After(first[i].Deadline))
	}
}

func (f fixture) expireLine(r draw.Round, slots []draw.Slot) {
	f.t.Helper()
	now := f.now()
	for i := range slots {
		slots[i].Deadline = now.Add(-time.Second)
	}
	f.rewriteSlots(r, slots, now)
}

func (f fixture) now() time.Time {
	f.t.Helper()
	now, err := valkey.ServerNow(f.t.Context(), f.client)
	require.NoError(f.t, err)
	return now
}

func (f fixture) rewriteSlots(r draw.Round, slots []draw.Slot, wake time.Time) {
	f.t.Helper()
	batch := valkey.SlotBatch{RoundID: string(r.ID), Phase: "settling",
		Wake: valkey.DrawWake{Channel: "draw:due:wake", At: wake}}
	for _, slot := range slots {
		raw, err := codec.Marshal(slot)
		require.NoError(f.t, err)
		batch.Shifts = append(batch.Shifts, valkey.SlotShift{Slot: strconv.Itoa(slot.N), Next: string(raw),
			Expect: valkey.SlotStamp{State: string(slot.State), Generation: slot.Generation}})
	}
	applied, err := drawKeys(r).ShiftSlotBatch(f.t.Context(), f.client, batch)
	require.NoError(f.t, err)
	require.True(f.t, applied)
}

func (f fixture) claimOldGeneration(r draw.Round, slot draw.Slot) {
	f.t.Helper()
	field := valkey.HashField{Key: drawKeys(r).Claimable, Field: string(slot.Viewer)}
	current := fmt.Sprintf("%s:%d:%d", r.ID, slot.N, slot.Generation)
	stale := fmt.Sprintf("%s:%d:%d", r.ID, slot.N, slot.Generation-1)
	for _, test := range []struct {
		ref  string
		want int
	}{{stale, 0}, {current, 1}} {
		value, err := codec.Marshal([]string{test.ref})
		require.NoError(f.t, err)
		require.NoError(f.t, field.Write(f.t.Context(), f.client, valkey.HashWrite{Value: string(value), TTL: time.Hour, Replace: true}))
		result, err := f.Claim(f.t.Context(), r.Channel, slot.Viewer)
		require.NoError(f.t, err)
		require.Equal(f.t, draw.ClaimOutcome{Claimed: test.want}, result)
	}
}

func TestDrawExtendKeepsOpeningTermsAndHonorsWindow(t *testing.T) {
	for _, tc := range []struct {
		name, snapshotID string
		window           time.Duration
		applied          bool
		wantErr          error
	}{
		{name: "within the policy window", window: time.Minute, applied: true},
		{name: "beyond the policy window", window: draw.Pins.GiveawayMaxEntryWindow},
		{name: "exactly at snapshot expiry", snapshotID: "snapshot", window: draw.Pins.GiveawayMaxEntryWindow - time.Minute, applied: true},
		{name: "beyond snapshot expiry", snapshotID: "snapshot", window: draw.Pins.GiveawayMaxEntryWindow - time.Second, wantErr: draw.ErrSnapshotExpiry},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, draw.Config{})
			r := f.openFor(407, draw.Terms{Kind: draw.KindGiveaway, Prize: draw.Prize{Item: "poster"}, SnapshotID: tc.snapshotID}, tc.window)
			applied, err := f.Extend(t.Context(), draw.ExtendRequest{Channel: r.Channel, Actor: "mod"})
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.applied, applied)
			f.checkExtension(r, applied)
		})
	}
}

func (f fixture) checkExtension(r draw.Round, applied bool) {
	f.t.Helper()
	got, found, err := f.Joinable(f.t.Context(), r.Channel)
	require.NoError(f.t, err)
	require.True(f.t, found)
	if applied {
		r.ClosesAt = r.ClosesAt.Add(time.Minute)
		require.Len(f.t, got.Actions, 1)
		r.Actions = []draw.Action{{Kind: "extend", Actor: "mod", At: got.Actions[0].At,
			Value: strconv.FormatInt(r.ClosesAt.UnixMilli(), 10)}}
	}
	require.Equal(f.t, r, got)
}

func TestDrawTransientPaymentStopsAtHorizon(t *testing.T) {
	payer := testPayer{paid: make(chan draw.Payment, 3), err: errors.New("credits unavailable")}
	ann := make(notices, 4)
	f := newFixture(t, draw.Config{Payer: payer, Announcer: ann, Verifier: verifyWith(verifyFirstCandidate)})
	r := f.open(408, draw.Terms{Kind: draw.KindRaffle, Prize: draw.Prize{Points: 11}, Winners: draw.WinnerPolicy{Count: 2}})
	f.join(r, "11")
	f.join(r, "12")
	f.close(r.Channel)
	f.start(f.Store)
	attempt := take(t, payer.paid)
	require.Equal(t, draw.ViewerID("12"), attempt.Viewer)
	f.awaitRaffleRetries(r.Channel)
	f.ageRaffle(r)
	unpaid := ann.next(t, draw.NoticeUnpaid).Slots
	require.Len(t, unpaid, 1)
	require.Equal(t, attempt.Viewer, unpaid[0].Viewer)
	waitRemoved(t, f.client, drawKeys(r))
	require.Empty(t, payer.paid)
}

func (f fixture) awaitRaffleRetries(channel draw.ChannelID) {
	f.t.Helper()
	require.EventuallyWithT(f.t, func(check *assert.CollectT) {
		_, slots, found, err := f.Settling(f.t.Context(), channel, draw.KindRaffle)
		require.NoError(check, err)
		require.Len(check, slots, 2)
		type observed struct {
			byState map[draw.SlotState]bool
			found   bool
		}
		byState := make(map[draw.SlotState]bool)
		for _, slot := range slots {
			byState[slot.State] = !slot.RetryAt.IsZero()
		}
		require.Equal(check, observed{map[draw.SlotState]bool{draw.SlotWon: true, draw.SlotVerifying: true}, true}, observed{byState, found})
	}, 5*time.Second, 10*time.Millisecond)
}

func TestDrawAccountInstanceFailure(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		permanent bool
	}{
		{"retired account", fmt.Errorf("lookup: %w", draw.ErrAccountInstanceUnavailable), true},
		{"transient lookup error", context.DeadlineExceeded, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payer := testPayer{instanceErr: tc.err, paid: make(chan draw.Payment, 1)}
			ann := make(notices, 4)
			f := newFixture(t, draw.Config{Payer: payer, Announcer: ann})
			r := f.open(409, draw.Terms{Kind: draw.KindRaffle, Prize: draw.Prize{Points: 11}})
			f.join(r, "11")
			f.close(r.Channel)
			f.start(f.Store)
			drawn := take(t, ann)
			require.Equal(t, draw.Notice{Event: draw.NoticeDrawn, Round: drawn.Round, Entries: 1}, drawn)
			if tc.permanent {
				waitRemoved(t, f.client, drawKeys(r))
				require.Empty(t, ann)
			} else {
				f.awaitSlot(r, func(slot draw.Slot) bool { return slot.State == draw.SlotPending && !slot.RetryAt.IsZero() })
			}
			require.Empty(t, payer.paid)
		})
	}
}

func (f fixture) ageRaffle(r draw.Round) {
	f.t.Helper()
	ctx := f.t.Context()
	now, err := valkey.ServerNow(ctx, f.client)
	require.NoError(f.t, err)
	old := now.Add(-draw.Pins.PayoutHorizon - time.Minute)
	applied, err := drawKeys(r).Shift(ctx, f.client, valkey.PhaseShift{
		From: "settling", To: "settling", RoundID: string(r.ID), TTL: time.Hour,
		Fields: []valkey.FieldValue{{Field: "settled_at", Value: strconv.FormatInt(old.UnixMilli(), 10)}},
		Wake:   valkey.DrawWake{At: now, Channel: "draw:due:wake"},
	})
	require.NoError(f.t, err)
	require.True(f.t, applied)
}

func TestDrawStoreRequiresPorts(t *testing.T) {
	cases := []struct {
		name   string
		remove func(*draw.Config)
	}{
		{"verifier", func(cfg *draw.Config) { cfg.Verifier = nil }},
		{"payer", func(cfg *draw.Config) { cfg.Payer = nil }},
		{"announcer", func(cfg *draw.Config) { cfg.Announcer = nil }},
		{"winner sink", func(cfg *draw.Config) { cfg.Winners = nil }},
		{"phase observer", func(cfg *draw.Config) { cfg.Phases = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(draw.Config{})
			tc.remove(&cfg)
			require.Panics(t, func() { draw.NewStore(nil, cfg) })
		})
	}
}
