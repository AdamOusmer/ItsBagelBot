package worker

import (
	"context"
	"net/http"
	"testing"

	"ItsBagelBot/app/outgress/internal/channels"
	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

// The dispatch benchmarks mirror sesame's pipeline_bench_test.go contract: on
// a warm pod the entire pre-Twitch path — decode, pause check, action lookup,
// route fill, registry read, rate take, body assembly — is in-process, so the
// only wait a chat send pays is its own Twitch round trip. The Twitch call is
// pinned to a canned in-process transport here, which makes any regression
// that puts network or heavy allocation back on the path show up as ns/op and
// allocs/op movement.

// allowAll is the warm-path limiter stand-in: the production LeaseManager's
// common case is an in-process token-bucket hit, so a constant admit models it
// without Valkey.
type allowAll struct{}

func (allowAll) Allow(context.Context, ratelimit.Request) (bool, error) { return true, nil }
func (allowAll) AllowOrdered(context.Context, ratelimit.Request, ratelimit.Request) (uint8, error) {
	return 0, nil
}

type cannedTransport struct{ status int }

func (t cannedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: t.status, Body: http.NoBody}, nil
}

// benchWorker assembles a lane worker whose collaborators answer without any
// network: seeded pause snapshot, primed channel cache, constant-admit
// limiter, static tokens, and a canned Twitch transport.
func benchWorker(tb testing.TB) *Worker {
	tb.Helper()
	registry := channels.New(nil)
	registry.SeedPause(false)
	registry.Prime(manage.Channel{BroadcasterID: "44322889", Enabled: true, IsMod: true})

	tw := twitch.NewClient("bench-client-id",
		twitch.NewStaticTokenSource("app-token"),
		twitch.NewStaticTokenSource("bot-token"), nil)
	tw.SetTransport(cannedTransport{status: http.StatusNoContent})

	return New(Config{
		Log:      zap.NewNop(),
		Limiter:  allowAll{},
		Registry: registry,
		Twitch:   tw,
		BotID:    "987654",
		Lane:     LanePremium,
	})
}

const benchChatBody = `{"type":"chat","broadcaster_id":"44322889","payload":{"broadcaster_id":"44322889","message":"hello chat"}}`

func BenchmarkProcessChat(b *testing.B) {
	w := benchWorker(b)
	payload := []byte(benchChatBody)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := w.Process(bus.NewMessage("bench", payload)); err != nil {
			b.Fatal(err)
		}
	}
}

// TestProcessChatAllocCeiling guards the hot path's allocation budget the same
// way sesame's TestProcessNoOutputAllocCeiling does: the ceiling is
// deliberately loose (decoder floor, sender-id splice, one HTTP request
// through the canned transport), so it only trips on structural regressions —
// a re-marshal sneaking into the body path, per-message registry rebuilding,
// or dispatch falling back to reflection.
func TestProcessChatAllocCeiling(t *testing.T) {
	w := benchWorker(t)
	payload := []byte(benchChatBody)
	// Loose on purpose: ~43 allocs/op on arm64 today, with headroom for cache
	// internals and sonic's per-platform decoder differences. A structural
	// regression (an extra marshal round-trip, per-message registry work)
	// costs well more than the slack.
	const ceiling = 96.0

	allocs := testing.AllocsPerRun(500, func() {
		if err := w.Process(bus.NewMessage("bench", payload)); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > ceiling {
		t.Fatalf("chat hot path allocates %.1f allocs/op, ceiling %.0f", allocs, ceiling)
	}
}
