package worker

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

// scriptedLimiter answers Allow per key: denied keys report exhaustion,
// error keys report an infra failure, everything else is admitted. Keys are
// recorded in call order so tests can assert the reserve-then-general draw.
type scriptedLimiter struct {
	denied map[string]bool
	errs   map[string]error
	calls  []string
}

func (s *scriptedLimiter) Allow(_ context.Context, req ratelimit.Request) (bool, error) {
	s.calls = append(s.calls, req.Key)
	if err := s.errs[req.Key]; err != nil {
		return false, err
	}
	return !s.denied[req.Key], nil
}

func (s *scriptedLimiter) AllowOrdered(context.Context, ratelimit.Request, ratelimit.Request) (uint8, error) {
	return 0, nil
}

func systemLaneWorker(limiter ratelimit.Manager) *Worker {
	return New(Config{Log: zap.NewNop(), Limiter: limiter, Lane: LaneSystem})
}

func TestTakeSystemHelixPrefersReserve(t *testing.T) {
	limiter := &scriptedLimiter{}
	w := systemLaneWorker(limiter)

	if err := w.takeSystemHelix(context.Background()); err != nil {
		t.Fatalf("takeSystemHelix() = %v, want nil", err)
	}
	if len(limiter.calls) != 1 || limiter.calls[0] != "ratelimit:helix:system" {
		t.Fatalf("calls = %v, want only the system reserve", limiter.calls)
	}
}

func TestTakeSystemHelixSpillsToGeneralWhenReserveDrained(t *testing.T) {
	limiter := &scriptedLimiter{denied: map[string]bool{"ratelimit:helix:system": true}}
	w := systemLaneWorker(limiter)

	if err := w.takeSystemHelix(context.Background()); err != nil {
		t.Fatalf("takeSystemHelix() = %v, want nil via general spillover", err)
	}
	want := []string{"ratelimit:helix:system", "ratelimit:helix:app"}
	if len(limiter.calls) != 2 || limiter.calls[0] != want[0] || limiter.calls[1] != want[1] {
		t.Fatalf("calls = %v, want %v", limiter.calls, want)
	}
}

func TestTakeSystemHelixDeniedWhenBothDrained(t *testing.T) {
	limiter := &scriptedLimiter{denied: map[string]bool{
		"ratelimit:helix:system": true,
		"ratelimit:helix:app":    true,
	}}
	w := systemLaneWorker(limiter)

	if err := w.takeSystemHelix(context.Background()); !errors.Is(err, errRateLimitShared) {
		t.Fatalf("takeSystemHelix() = %v, want errRateLimitShared", err)
	}
}

func TestTakeSystemHelixInfraErrorDoesNotSpill(t *testing.T) {
	boom := errors.New("valkey down")
	limiter := &scriptedLimiter{errs: map[string]error{"ratelimit:helix:system": boom}}
	w := systemLaneWorker(limiter)

	if err := w.takeSystemHelix(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("takeSystemHelix() = %v, want infra error passthrough", err)
	}
	if len(limiter.calls) != 1 {
		t.Fatalf("calls = %v, want no spillover on infra error", limiter.calls)
	}
}
