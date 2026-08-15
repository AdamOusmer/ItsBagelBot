package idempotency

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/pkg/bus"

	valkey_go "github.com/valkey-io/valkey-go"
)

// fakeStore is an in-memory Store for the LRU, composite and Guard tests. It
// counts Seen/Release calls and can be made to fail open on demand.
type fakeStore struct {
	mu       sync.Mutex
	claims   map[string]bool
	seenN    int
	releaseN int
	err      error
}

func newFakeStore() *fakeStore { return &fakeStore{claims: map[string]bool{}} }

func (f *fakeStore) Seen(_ context.Context, key string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenN++
	if f.err != nil {
		return false, f.err // fail open: not-seen plus the error
	}
	if f.claims[key] {
		return true, nil
	}
	f.claims[key] = true
	return false, nil
}

func (f *fakeStore) Release(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseN++
	delete(f.claims, key)
	return nil
}

func (f *fakeStore) held(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.claims[key]
}

// --- Store dedup semantics (via the fake, exercised through the composite) ---

func TestStoreDedupSemantics(t *testing.T) {
	f := newFakeStore()
	ctx := context.Background()

	seen, err := f.Seen(ctx, "k", time.Minute)
	if err != nil || seen {
		t.Fatalf("first claim: seen=%v err=%v, want false/nil", seen, err)
	}
	seen, err = f.Seen(ctx, "k", time.Minute)
	if err != nil || !seen {
		t.Fatalf("second claim: seen=%v err=%v, want true/nil", seen, err)
	}
	// Release frees the key for a fresh claim.
	if err := f.Release(ctx, "k"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if seen, _ := f.Seen(ctx, "k", time.Minute); seen {
		t.Fatal("claim after release should be fresh, not a duplicate")
	}
}

// --- ValkeyStore result classification + ttl flooring (no live Valkey) ---

func TestSetNXOutcome(t *testing.T) {
	if seen, ok := setNXOutcome(nil); seen || !ok {
		t.Fatalf("nil err: seen=%v ok=%v, want false/true (fresh claim)", seen, ok)
	}
	if seen, ok := setNXOutcome(valkey_go.Nil); !seen || !ok {
		t.Fatalf("valkey nil: seen=%v ok=%v, want true/true (duplicate)", seen, ok)
	}
	if seen, ok := setNXOutcome(errors.New("connection refused")); seen || ok {
		t.Fatalf("infra err: seen=%v ok=%v, want false/false (fail open)", seen, ok)
	}
}

func TestMillisFloor(t *testing.T) {
	if got := millis(0); got != 1 {
		t.Fatalf("millis(0)=%d, want floor 1", got)
	}
	if got := millis(500 * time.Microsecond); got != 1 {
		t.Fatalf("millis(500us)=%d, want floor 1", got)
	}
	if got := millis(2 * time.Second); got != 2000 {
		t.Fatalf("millis(2s)=%d, want 2000", got)
	}
}

// --- LRU eviction + TTL ---

func TestLRUEviction(t *testing.T) {
	c := newTTLLRU(2, time.Now)
	c.add("a", time.Minute)
	c.add("b", time.Minute)
	c.add("c", time.Minute) // evicts the oldest, "a"
	if c.has("a") {
		t.Fatal("a should have been evicted at capacity")
	}
	if !c.has("b") || !c.has("c") {
		t.Fatal("b and c should still be present")
	}
	if c.len() != 2 {
		t.Fatalf("len=%d, want 2", c.len())
	}
}

func TestLRURecencyKeepsHotKey(t *testing.T) {
	c := newTTLLRU(2, time.Now)
	c.add("a", time.Minute)
	c.add("b", time.Minute)
	_ = c.has("a")          // touch "a" so "b" is now the oldest
	c.add("c", time.Minute) // evicts "b"
	if c.has("b") {
		t.Fatal("b should have been evicted, not the recently-used a")
	}
	if !c.has("a") {
		t.Fatal("recently-used a should survive")
	}
}

func TestLRUTTLExpiry(t *testing.T) {
	now := time.Unix(0, 0)
	c := newTTLLRU(8, func() time.Time { return now })
	c.add("a", time.Minute)
	if !c.has("a") {
		t.Fatal("a should be live immediately")
	}
	now = now.Add(2 * time.Minute) // past the ttl
	if c.has("a") {
		t.Fatal("a should have expired")
	}
	if c.len() != 0 {
		t.Fatalf("expired entry should be dropped, len=%d", c.len())
	}
}

// --- Composite tier short-circuit ---

func TestCompositeShortCircuit(t *testing.T) {
	f := newFakeStore()
	store := NewTiered(100, f)
	ctx := context.Background()

	if seen, _ := store.Seen(ctx, "k", time.Minute); seen {
		t.Fatal("first Seen should be a fresh claim")
	}
	if seen, _ := store.Seen(ctx, "k", time.Minute); !seen {
		t.Fatal("second Seen should be a duplicate")
	}
	// The duplicate was served from the local LRU: inner saw exactly one call.
	if f.seenN != 1 {
		t.Fatalf("inner Seen calls=%d, want 1 (second served from LRU)", f.seenN)
	}
}

func TestCompositeReleaseClearsLocal(t *testing.T) {
	f := newFakeStore()
	store := NewTiered(100, f)
	ctx := context.Background()

	_, _ = store.Seen(ctx, "k", time.Minute)
	if err := store.Release(ctx, "k"); err != nil {
		t.Fatalf("release: %v", err)
	}
	// After release the local claim is gone, so the next Seen reaches inner again.
	if seen, _ := store.Seen(ctx, "k", time.Minute); seen {
		t.Fatal("claim after release should be fresh")
	}
	if f.seenN != 2 {
		t.Fatalf("inner Seen calls=%d, want 2 (LRU cleared on release)", f.seenN)
	}
}

func TestCompositeFailOpenNotCached(t *testing.T) {
	f := newFakeStore()
	f.err = errors.New("valkey down")
	store := NewTiered(100, f)
	ctx := context.Background()

	// Both deliveries fail open and neither is cached, so both reach inner (a
	// retry stays free to claim once the backend recovers).
	if seen, _ := store.Seen(ctx, "k", time.Minute); seen {
		t.Fatal("fail-open Seen should report not-seen")
	}
	if seen, _ := store.Seen(ctx, "k", time.Minute); seen {
		t.Fatal("second fail-open Seen should still report not-seen")
	}
	if f.seenN != 2 {
		t.Fatalf("inner Seen calls=%d, want 2 (fail-open not cached)", f.seenN)
	}
}

// --- Guard ---

type countMetrics struct {
	dup      int
	failOpen int
}

func (m *countMetrics) Duplicate() { m.dup++ }
func (m *countMetrics) FailOpen()  { m.failOpen++ }

func TestGuardDuplicateSkip(t *testing.T) {
	f := newFakeStore()
	m := &countMetrics{}
	calls := 0
	handler := func(*bus.Message) error { calls++; return nil }

	guarded := Guard(Config{Store: f, Key: MessageUUIDKey, TTL: time.Minute, Metrics: m})(handler)

	msg := bus.NewMessage("evt-1", nil)
	if err := guarded(msg); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if err := guarded(bus.NewMessage("evt-1", nil)); err != nil {
		t.Fatalf("duplicate delivery: %v", err)
	}
	if calls != 1 {
		t.Fatalf("handler ran %d times, want 1 (duplicate skipped)", calls)
	}
	if m.dup != 1 {
		t.Fatalf("duplicate metric=%d, want 1", m.dup)
	}
}

func TestGuardErrorReleases(t *testing.T) {
	f := newFakeStore()
	boom := errors.New("handler failed")
	handler := func(*bus.Message) error { return boom }

	guarded := Guard(Config{Store: f, Key: MessageUUIDKey, TTL: time.Minute})(handler)

	if err := guarded(bus.NewMessage("evt-2", nil)); !errors.Is(err, boom) {
		t.Fatalf("guard should propagate the handler error, got %v", err)
	}
	// The failed claim was released, so the event is not permanently suppressed.
	if f.held("evt-2") {
		t.Fatal("claim should be released after the handler failed")
	}
	if f.releaseN != 1 {
		t.Fatalf("release calls=%d, want 1", f.releaseN)
	}

	// A redelivery of the failed event runs the handler again.
	ran := false
	retry := Guard(Config{Store: f, Key: MessageUUIDKey, TTL: time.Minute})(func(*bus.Message) error { ran = true; return nil })
	if err := retry(bus.NewMessage("evt-2", nil)); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	if !ran {
		t.Fatal("redelivery of a released claim should re-run the handler")
	}
}

func TestGuardFailOpenRunsHandler(t *testing.T) {
	f := newFakeStore()
	f.err = errors.New("valkey down")
	m := &countMetrics{}
	calls := 0
	guarded := Guard(Config{Store: f, Key: MessageUUIDKey, TTL: time.Minute, Metrics: m})(func(*bus.Message) error { calls++; return nil })

	// Two deliveries during the outage both run: a guard must never drop a live
	// event because its backend is unavailable.
	_ = guarded(bus.NewMessage("evt-3", nil))
	_ = guarded(bus.NewMessage("evt-3", nil))
	if calls != 2 {
		t.Fatalf("handler ran %d times, want 2 (fail open)", calls)
	}
	if m.failOpen != 2 {
		t.Fatalf("fail-open metric=%d, want 2", m.failOpen)
	}
}

func TestGuardUnguardablePassesThrough(t *testing.T) {
	f := newFakeStore()
	calls := 0
	guarded := Guard(Config{Store: f, Key: MessageUUIDKey, TTL: time.Minute})(func(*bus.Message) error { calls++; return nil })

	// An empty UUID is not guardable: the handler runs and the store is untouched.
	if err := guarded(bus.NewMessage("", nil)); err != nil {
		t.Fatalf("unguardable delivery: %v", err)
	}
	if calls != 1 {
		t.Fatalf("handler ran %d times, want 1", calls)
	}
	if f.seenN != 0 {
		t.Fatalf("store consulted %d times for an unguardable message, want 0", f.seenN)
	}
}

// TestGuardConcurrentDuplicates asserts the claim-before order admits exactly one
// of two copies of the same event racing on one pod.
func TestGuardConcurrentDuplicates(t *testing.T) {
	f := newFakeStore()
	var ran int32
	var mu sync.Mutex
	handler := func(*bus.Message) error {
		mu.Lock()
		ran++
		mu.Unlock()
		return nil
	}
	guarded := Guard(Config{Store: f, Key: MessageUUIDKey, TTL: time.Minute})(handler)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = guarded(bus.NewMessage("evt-race", nil))
		}()
	}
	wg.Wait()
	if ran != 1 {
		t.Fatalf("handler ran %d times, want exactly 1 across the race", ran)
	}
}
