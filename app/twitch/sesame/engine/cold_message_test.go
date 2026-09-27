package engine

import (
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
	"time"
)

type countingLocaleReader struct {
	fakeReader
	users int
}

func (r *countingLocaleReader) User(ctx context.Context, id uint64) (projection.User, error) {
	r.users++
	return r.fakeReader.User(ctx, id)
}

type countingChannelReader struct {
	countingLocaleReader
	loads       int
	needModules bool
}

func (r *countingChannelReader) LoadChannel(_ context.Context, _ uint64, needModules bool) (map[string]projection.ModuleView, projection.User, error) {
	r.loads++
	r.needModules = needModules
	return r.modules, r.user, r.modErr
}

func localePing() module.Module {
	m := module.NewModule("", module.KindCore)
	m.Command("ping").Everyone().Run(func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		c.EnsureLocale(ctx) // an additional handler request must not repeat an empty locale read
		emit(&module.Output{Type: "chat", BroadcasterID: c.Env.BroadcasterUserID, Text: "locale=" + c.Locale})
		return nil
	})
	m.On(chatType, func(ctx context.Context, c *module.Context, _ module.Emit) error {
		if c.Command != "" {
			c.EnsureLocale(ctx)
		}
		return nil
	})
	return m.Build()
}

func TestPlainChatDoesNotReadLocale(t *testing.T) {
	reader := &countingChannelReader{}
	m := module.NewModule("voice", module.KindDefault)
	m.On(chatType, func(context.Context, *module.Context, module.Emit) error { return nil })
	p := newPipelineWith(&fakePublisher{}, reader, m.Build())
	require.NoError(t, p.Process(chatMsg(t, "standard", "hello")))
	require.Zero(t, reader.users)
	require.Zero(t, reader.loads)
}

func TestBakedCommandLocaleLoadedOnceIncludingDefault(t *testing.T) {
	for _, locale := range []string{"", "fr"} {
		t.Run(locale, func(t *testing.T) {
			reader := &countingLocaleReader{fakeReader: fakeReader{user: projection.User{Locale: locale}}}
			pub := &fakePublisher{}
			p := newPipelineWith(pub, reader, localePing())
			require.NoError(t, p.Process(chatMsg(t, "standard", "!ping")))
			require.Equal(t, 1, reader.users)
			require.Equal(t, "locale="+locale, chatMessageText(t, pub.got[0].msg))
			require.Equal(t, locale, pub.got[0].msg.Locale)
		})
	}
}

func TestBakedCommandUsesOptionalChannelLoader(t *testing.T) {
	reader := &countingChannelReader{countingLocaleReader: countingLocaleReader{fakeReader: fakeReader{user: projection.User{Locale: "fr"}}}}
	pub := &fakePublisher{}
	p := newPipelineWith(pub, reader, localePing(), bareModule("views", module.KindDefault))
	// A named command owner makes the registry require modules on chat.
	m := module.NewModule("named", module.KindDefault)
	m.Command("named").Everyone().Run(func(context.Context, *module.Context, string, module.Emit) error { return nil })
	p.registry = NewRegistry(p.log, localePing(), m.Build())
	require.NoError(t, p.Process(chatMsg(t, "standard", "!ping")))
	require.Equal(t, 1, reader.loads)
	require.True(t, reader.needModules)
	require.Zero(t, reader.users)
	require.Equal(t, "locale=fr", chatMessageText(t, pub.got[0].msg))
}

func trialMessage(t *testing.T, eventType, id, text string) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(map[string]any{"type": eventType, "lane": "standard", "broadcaster_user_id": id, "chatter_user_id": "999", "text": text, "origin": "trial", "trial_generation": 7})
	require.NoError(t, err)
	return bus.NewMessage("trial-test", body)
}

func TestTrialNeverReadsAccountForHandlersOrCommands(t *testing.T) {
	for _, text := range []string{"hello", "!ping"} {
		t.Run(text, func(t *testing.T) {
			reader := &countingChannelReader{countingLocaleReader: countingLocaleReader{fakeReader: fakeReader{user: projection.User{Locale: "fr"}}}}
			pub := &fakePublisher{}
			p := newPipelineWith(pub, reader, localePing(), emitModule("voice", module.KindDefault, "fixed"))
			require.NoError(t, p.Process(trialMessage(t, chatType, "123", text)))
			require.Zero(t, reader.users)
			require.Zero(t, reader.loads)
			for _, got := range pub.got {
				require.Equal(t, "trial", got.msg.Origin)
				require.Equal(t, uint64(7), got.msg.TrialGeneration)
			}
			if text == "!ping" {
				require.Equal(t, "locale=", chatMessageText(t, pub.got[0].msg))
			}
		})
	}
}

func trialCohort(t *testing.T) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(map[string]any{"type": chatType, "lane": "standard", "broadcaster_user_id": "123", "text": "hello", "origin": "trial", "senders": []map[string]any{{"chatter_user_id": "1"}, {"chatter_user_id": "2"}, {"chatter_user_id": "3"}}})
	require.NoError(t, err)
	return bus.NewMessage("trial-cohort", body)
}
func TestColdTrialCountersPreserveWeightedFailuresAndFilteredCounts(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run("failed="+strconv.FormatBool(failed), func(t *testing.T) {
			reader := fakeReader{}
			if failed {
				reader.modErr = errors.New("projection unavailable")
			}
			p := newPipelineWith(&fakePublisher{}, reader, emitModule("named", module.KindDefault, "hello"))
			counts := &recordingBumper{got: map[string]int64{}}
			p.trialCounts = counts
			err := p.Process(trialCohort(t))
			require.Equal(t, int64(3), counts.got["trial_decoded"])
			require.Equal(t, int64(1), counts.got["trial_latency_ns_samples"])
			require.Positive(t, counts.got["trial_latency_ns_total"])
			if failed {
				require.ErrorIs(t, err, reader.modErr)
				require.Equal(t, int64(3), counts.got["trial_failed"])
				require.Equal(t, int64(3), counts.got["trial_retried"])
				require.Zero(t, counts.got["trial_processed"])
			} else {
				require.NoError(t, err)
				require.Equal(t, int64(3), counts.got["trial_processed"])
			}
		})
	}
	p := newPipelineWith(&fakePublisher{}, fakeReader{})
	counts := &recordingBumper{got: map[string]int64{}}
	p.trialCounts = counts
	require.NoError(t, p.Process(trialMessage(t, "unhandled", "123", "hello")))
	require.Equal(t, map[string]int64{"trial_decoded": 1}, counts.got)
}

type slowTrialBumper struct{ recordingBumper }

func (b *slowTrialBumper) BumpChannel(id uint64, name string, amount int64) {
	if name == "trial_processed" {
		time.Sleep(100 * time.Millisecond)
	}
	b.recordingBumper.BumpChannel(id, name, amount)
}
func TestTrialDurationCapturedBeforeOutcomeReporting(t *testing.T) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{})
	counts := &slowTrialBumper{recordingBumper{got: map[string]int64{}}}
	p.trialCounts = counts
	require.NoError(t, p.Process(chatMsg(t, "standard", "warm codec")))
	require.NoError(t, p.Process(trialMessage(t, chatType, "123", "hello")))
	require.Less(t, counts.got["trial_latency_ns_total"], int64(75*time.Millisecond))
	require.Equal(t, int64(1), counts.got["trial_processed"])
}

type coldLocaleObserver struct{ events chan ObservedEvent }

func (o coldLocaleObserver) Observe(ev ObservedEvent) { o.events <- ev }
func TestGatedCommandObserverKeepsLocale(t *testing.T) {
	m := module.NewModule("", module.KindCore)
	m.Command("private").AllowUser("777").Run(func(context.Context, *module.Context, string, module.Emit) error {
		t.Fatal("permission denied body must not run")
		return nil
	})
	reader := &countingLocaleReader{fakeReader: fakeReader{user: projection.User{Locale: "fr"}}}
	p := newPipelineWith(&fakePublisher{}, reader, m.Build())
	events := make(chan ObservedEvent, 1)
	p.RegisterObserver(coldLocaleObserver{events: events})
	defer p.Close()
	require.NoError(t, p.Process(chatMsg(t, "standard", "!private")))
	select {
	case event := <-events:
		require.Equal(t, "fr", event.Locale)
		require.Equal(t, "private", event.Command)
	case <-time.After(time.Second):
		t.Fatal("missing command observation")
	}
	require.Equal(t, 1, reader.users)
}
