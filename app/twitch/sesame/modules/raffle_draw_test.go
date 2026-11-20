// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type rafflePayCapture struct {
	sync.Mutex
	payments map[draw.PaymentKey]draw.Payment
	calls    int
	loseAck  bool
}

func (*rafflePayCapture) Instance(context.Context, draw.ChannelID, draw.ViewerID) (int64, error) {
	return 1, nil
}

func (p *rafflePayCapture) Pay(_ context.Context, payment draw.Payment) error {
	p.Lock()
	defer p.Unlock()
	p.calls++
	if _, paid := p.payments[payment.Key]; paid {
		return nil
	}
	p.payments[payment.Key] = payment
	if p.loseAck {
		p.loseAck = false
		return fmt.Errorf("credit reply lost")
	}
	return nil
}

type raffleFixture struct {
	t      *testing.T
	draw   *draw.Store
	freeze *freezeStore
	proj   *fakeProj
	pay    *rafflePayCapture
	notice *variablePublisher
	events *variablePublisher
	pipe   *engine.Pipeline
}

func newRaffleFixture(t *testing.T, loyaltyOn, paused bool) raffleFixture {
	t.Helper()
	log := zap.NewNop()
	client := valkeytest.Spawn(t)
	proj := &fakeProj{modules: []projection.ModuleView{
		{Name: engine.LoyaltyModuleName, IsEnabled: loyaltyOn},
		{Name: raffleModuleName, IsEnabled: true, Configs: []byte(raffleJoinOn)},
	}}
	pay := &rafflePayCapture{payments: make(map[draw.PaymentKey]draw.Payment)}
	notice := &variablePublisher{}
	checker := eligibility.NewChecker(eligibility.Ports{})
	t.Cleanup(checker.Close)
	store := draw.NewStore(client, draw.Config{Verifier: checker, Payer: pay, Winners: draw.DiscardWinnerRecords{},
		Announcer: engine.NewDrawAnnouncer(engine.AnnouncementConfig{Proj: proj, Pub: notice}, log, nil),
		Phases:    draw.IgnorePhaseChanges{}})
	events := &variablePublisher{}
	freeze := &freezeStore{active: paused}
	deps := engine.Deps{Draw: store, Eligibility: checker, Proj: proj, Pub: events, Log: log, LoyaltyFreeze: freeze, Cooldown: engine.NoopCooldown{}}
	pipe := engine.NewPipeline(deps, engine.NewRegistry(log, All(deps)...), engine.Config{OutgressStandard: "outgress.standard"})
	t.Cleanup(pipe.Close)
	return raffleFixture{t: t, draw: store, freeze: freeze,
		proj: proj, pay: pay, notice: notice, events: events, pipe: pipe}
}

func (f raffleFixture) run(channel draw.ChannelID, chatter *lane.Envelope, command string) []string {
	f.t.Helper()
	f.events.messages = nil
	chatter.BroadcasterUserID = strconv.FormatUint(uint64(channel), 10)
	chatter.Lane = "standard"
	chatter.Text = command
	body, err := codec.Marshal(chatter)
	require.NoError(f.t, err)
	require.NoError(f.t, f.pipe.Process(bus.NewMessage("raffle-test", body)))
	return drawReplies(f.t, f.events)
}

func TestRaffleProcessOpensOnlyValidPointsRounds(t *testing.T) {
	for i, tc := range []struct {
		command  string
		want     draw.WinnerPolicy
		reply    string
		off      bool
		paused   bool
		busy     draw.Kind
		config   string
		currency string
	}{
		{command: "!raffle 5k 30", want: draw.WinnerPolicy{Count: 1}, reply: "5000 points"},
		{command: "!raffle 5k 30", want: draw.WinnerPolicy{Count: 1}, reply: "5000 bagels", currency: "bagels"},
		{command: "!sraffle 5k 30", want: draw.WinnerPolicy{Count: 1}, reply: "5000 points"},
		{command: "!sraffle 5k 30 2", reply: "Usage"},
		{command: "!mraffle 5k 30", want: draw.WinnerPolicy{Count: 3}, reply: "3 winners split 5000 points"},
		{command: "!mraffle 5k 30 5", want: draw.WinnerPolicy{Count: 5}, reply: "5000 points"},
		{command: "!mraffle 5k 30 auto", want: draw.WinnerPolicy{Auto: true}, reply: "Winners: 1 per 10 entries."},
		{command: "!raffle", config: `{"bareRaffle":"split"}`, want: draw.WinnerPolicy{Count: 3}, reply: "3 winners split 1000 points"},
		{command: "!raffle 5k60", reply: "Put a space between the amount and the seconds: !raffle 5k 60."},
		{command: "!mraffle 5k 30 101", reply: "Usage"},
		{command: "!mraffle 5k 30 3 extra", reply: "Usage"},
		{command: "!raffle 0", reply: "Usage"},
		{command: "!raffle", config: `{"prizePoints":0}`, want: draw.WinnerPolicy{Count: 1}, reply: "1000 points"},
		{command: "!raffle", config: `{"openedMessage":"Good luck with {raffle:amount} {raffle:points}!"}`,
			want: draw.WinnerPolicy{Count: 1}, reply: "1 winner takes 1000 points. Type !join (1m). Good luck with 1000 points!"},
		{command: "!sraffle", config: `{"prizePoints":-25}`, want: draw.WinnerPolicy{Count: 1}, reply: "1000 points"},
		{command: "!raffle", off: true, reply: "Loyalty must be on"},
		{command: "!raffle", paused: true, want: draw.WinnerPolicy{Count: 1}, reply: "1000 points"},
		{command: "!raffle", busy: draw.KindGiveaway, reply: "A giveaway is already open"},
		{command: "!raffle", busy: draw.KindRaffle, want: draw.WinnerPolicy{Count: 2}, reply: "A raffle is already running"},
	} {
		t.Run(tc.command+tc.reply, func(t *testing.T) {
			f := newRaffleFixture(t, !tc.off, tc.paused)
			f.configure(raffleFixtureConfig{raffle: tc.config, currency: tc.currency})
			ch := draw.ChannelID(80200 + i)
			f.occupy(ch, tc.busy)
			mod := chatCtx("42", "mod", "moderator").Env
			out := f.run(ch, &mod, tc.command)
			require.Len(t, out, 1)
			require.Contains(t, out[0], tc.reply)
			round, _, err := f.draw.Joinable(t.Context(), ch)
			require.NoError(t, err)
			assert.Equal(t, tc.want, rafflePolicy(round))
			assert.Zero(t, f.freeze.gets)
		})
	}
}

type raffleFixtureConfig struct {
	raffle, currency string
}

func (f raffleFixture) configure(cfg raffleFixtureConfig) {
	f.proj.modules[1].Configs = []byte(cfg.raffle)
	f.proj.modules[0].Configs = []byte(fmt.Sprintf(`{"pointsName":%q}`, cfg.currency))
}

func (f raffleFixture) occupy(ch draw.ChannelID, kind draw.Kind) {
	f.t.Helper()
	if kind == "" {
		return
	}
	terms := draw.Terms{Kind: kind, Winners: draw.WinnerPolicy{Count: 2}}
	if kind == draw.KindRaffle {
		terms.Prize.Points = 1000
	}
	_, result, err := f.draw.Open(f.t.Context(), draw.OpenRequest{Channel: ch, Duration: time.Minute, Terms: terms})
	require.NoError(f.t, err)
	require.Equal(f.t, draw.Opened, result)
}

func TestRaffleProcessDrawAutoAndIgnoresDrawWhileClosing(t *testing.T) {
	f := newRaffleFixture(t, true, false)
	const ch draw.ChannelID = 80600
	mod := chatCtx("42", "mod", "moderator").Env
	require.Len(t, f.run(ch, &mod, "!raffle 1000 60"), 1)
	for n := 1; n <= 11; n++ {
		viewer := chatCtx(strconv.Itoa(n), fmt.Sprintf("viewer%d", n)).Env
		require.Len(t, f.run(ch, &viewer, "!join"), 1)
	}
	for _, command := range []string{"!raffle draw auto", "!raffle draw 1"} {
		require.Empty(t, f.run(ch, &mod, command), command)
	}
	f.finish(ch)
	require.Len(t, f.pay.payments, 2)
	for _, payment := range f.pay.payments {
		assert.Equal(t, int64(500), payment.Points)
	}
}

func TestRaffleAnnouncementsRespectSettings(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		event        draw.NoticeEvent
		want         []string
	}{
		{"remind default", "", draw.NoticeRemind, []string{"Raffle reminder: ~1 min left! 1 entered so far. Type !join!"}},
		{"mid count", "", draw.NoticeMidCount, []string{"1 entered, 1m left. Type !join."}},
		{"remind off", `{"remindEnabled":"off"}`, draw.NoticeRemind, nil},
		{"won off", `{"wonEnabled":"off"}`, draw.NoticeWinners, nil},
		{"claim token falls back", `{"wonMessage":"Type !claim within {raffle:claim} min."}`, draw.NoticeWinners, []string{"@winner won 1000 bagels!"}},
		{"draw currency token", `{"wonMessage":"{raffle:targets} gets {raffle:amount} {draw:points}!"}`, draw.NoticeWinners, []string{"@winner gets 1000 bagels!"}},
		{"supported custom winner line", `{"wonMessage":"{raffle:targets} gets {raffle:amount} {raffle:points}!"}`, draw.NoticeWinners, []string{"@winner gets 1000 bagels!"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := &fakeProj{modules: []projection.ModuleView{
				{Name: engine.LoyaltyModuleName, IsEnabled: false, Configs: []byte(`{"pointsName":"bagels"}`)},
				{Name: raffleModuleName, IsEnabled: true, Configs: []byte(tc.config)},
			}}
			out := &variablePublisher{}
			announcer := engine.NewDrawAnnouncer(engine.AnnouncementConfig{Proj: proj, Pub: out}, zap.NewNop(), nil)
			announcer.Announce(t.Context(), draw.Notice{
				Event: tc.event, Entries: 1, TimeLeft: time.Minute, Slots: []draw.Slot{{Login: "winner"}},
				Round: draw.Round{Channel: 80700, Winners: 1, Terms: engine.RaffleTerms(engine.RaffleLaunch{Points: 1000})},
			})
			assert.Equal(t, tc.want, drawReplies(t, out))
		})
	}
}

func TestRaffleProcessCancelAndStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		other   draw.Kind
		actions []string
		lines   []string
	}{
		{"raffle status and cancel", "", []string{"!raffle 1000 60", "!raffle status", "!cancelraffle", "!raffle status"},
			[]string{"1 winner takes 1000 points", "0 entered", "cancelled", "No raffle is running"}},
		{"giveaway stays open", draw.KindGiveaway, []string{"!raffle draw 2", "!raffle cancel"},
			[]string{"No raffle is running", "No raffle is running"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRaffleFixture(t, true, false)
			const ch draw.ChannelID = 80400
			f.occupy(ch, tc.other)
			mod := chatCtx("42", "mod", "moderator").Env
			for i, cmd := range tc.actions {
				assert.Contains(t, fmt.Sprint(f.run(ch, &mod, cmd)), tc.lines[i])
			}
			_, open, err := f.draw.Joinable(t.Context(), ch)
			require.NoError(t, err)
			assert.Equal(t, tc.other == draw.KindGiveaway, open)
		})
	}
}

func rafflePolicy(r draw.Round) draw.WinnerPolicy {
	if r.Terms.Kind == draw.KindRaffle {
		return r.Terms.Winners
	}
	return draw.WinnerPolicy{}
}

func TestRaffleProcessSettlesAfterLoyaltyTurnsOff(t *testing.T) {
	for i, tc := range []struct {
		name, open, close string
		loseAck           bool
	}{
		{"configured count", "!mraffle 10k 60", "!raffle close", false},
		{"draw override", "!mraffle 10k 60 2", "!raffle draw 3", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRaffleFixture(t, true, false)
			ch := draw.ChannelID(80300 + i)
			mod := chatCtx("42", "mod", "moderator").Env
			require.Len(t, f.run(ch, &mod, tc.open), 1)
			for n := 1; n <= 3; n++ {
				entry := chatCtx(strconv.Itoa(n), fmt.Sprintf("viewer%d", n)).Env
				require.Len(t, f.run(ch, &entry, "!join"), 1)
			}
			f.settle(ch, tc.close, tc.loseAck)
		})
	}
}

func (f raffleFixture) settle(ch draw.ChannelID, command string, loseAck bool) {
	t := f.t
	t.Helper()
	f.proj.modules[0].IsEnabled = false
	f.pay.loseAck = loseAck
	mod := chatCtx("42", "mod", "moderator").Env
	require.Empty(t, f.run(ch, &mod, command))
	f.finish(ch)
	f.assertMultiPayout(loseAck)
}

func (f raffleFixture) finish(ch draw.ChannelID) {
	t := f.t
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.draw.Run(ctx)
	}()
	t.Cleanup(func() { <-done })
	require.Eventually(t, func() bool {
		live, err := f.draw.Live(ctx, ch)
		return err == nil && len(live) == 0
	}, 18*time.Second, 40*time.Millisecond)
	cancel()
	<-done
}

func (f raffleFixture) assertMultiPayout(loseAck bool) {
	t := f.t
	t.Helper()
	require.Len(t, f.pay.payments, 3)
	wantCalls := 3
	if loseAck {
		wantCalls++
	}
	assert.Equal(t, wantCalls, f.pay.calls)
	for _, payment := range f.pay.payments {
		assert.Equal(t, int64(3333), payment.Points)
	}
	var lines []string
	for _, line := range drawReplies(t, f.notice) {
		if strings.Contains(line, "each won") {
			lines = append(lines, line)
		}
	}
	require.Len(t, lines, 1)
	for _, name := range []string{"@viewer1", "@viewer2", "@viewer3", "each won 3333 points"} {
		assert.Contains(t, lines[0], name)
	}
}

func TestDrawBatchedRefusalLines(t *testing.T) {
	for _, tc := range []struct {
		name    string
		viewers []draw.RefusedViewer
	}{
		{"one line per reason", []draw.RefusedViewer{
			{Login: "b", Reason: "watch", Need: 7200}, {Login: "a", Reason: "watch", Need: 7200}, {Login: "c", Reason: "mod"},
		}},
		{"multiple lines for long mention lists", longRefusalBatch()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &variablePublisher{}
			announcer := engine.NewDrawAnnouncer(engine.AnnouncementConfig{Proj: &fakeProj{}, Pub: out}, zap.NewNop(), nil)
			announcer.Announce(t.Context(), draw.Notice{Event: draw.NoticeRefusals, Refused: tc.viewers,
				Round: draw.Round{Channel: 80220, Terms: draw.Terms{Kind: draw.KindGiveaway}}})
			assertRefusalBatch(t, drawReplies(t, out), tc.viewers)
		})
	}
}

func longRefusalBatch() []draw.RefusedViewer {
	var viewers []draw.RefusedViewer
	for i := 0; i < 40; i++ {
		viewers = append(viewers, draw.RefusedViewer{Login: fmt.Sprintf("watch_viewer_%020d", i), Reason: "watch", Need: 7200})
	}
	return viewers
}

func assertRefusalBatch(t *testing.T, lines []string, viewers []draw.RefusedViewer) {
	t.Helper()
	require.GreaterOrEqual(t, len(lines), 2)
	for _, line := range lines {
		require.LessOrEqual(t, len(line), validate.MaxResponseLineLength)
	}
	all := strings.Join(lines, "\n")
	for _, viewer := range viewers {
		require.Equal(t, 1, strings.Count(all, "@"+viewer.Login))
	}
	require.Contains(t, all, "need 2 hours of watch time. Type !check for details.")
}
