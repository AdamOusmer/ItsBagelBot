package main

import (
	"sync"
	"sync/atomic"
	"time"
)

// seqLedger is the delivery accounting: one cursor per ordered wire stream, two
// counters, no per-message state. Per (publisher, lane) rather than fleet-wide
// because two publisher replicas interleave on one stream, and a single global
// cursor would report every interleave as a gap.
type seqLedger struct {
	mu     sync.Mutex
	lanes  map[string]uint64
	counts seqCounts
}

// seqCounts separates the two ways a receipt-level lane deviates from what was
// published. Gaps are messages this pod will never see — the flow consumer has
// no redelivery, so a gap is a permanent loss and the number that bounds how
// much a handler may assume. Regressions are a sequence arriving at or below the
// cursor: a JetStream redelivery, or a reorder. They are counted apart because
// only one of them is recoverable and the guard is what absorbs it.
type seqCounts struct {
	Delivered   int64 `json:"delivered"`
	Gaps        int64 `json:"gaps"`
	Regressions int64 `json:"regressions"`
	Lanes       int   `json:"lanes"`
}

func newSeqLedger() *seqLedger {
	return &seqLedger{lanes: make(map[string]uint64)}
}

// observe records one delivery. The first sequence seen on a lane only seeds the
// cursor: the consumer's flow consumer opens at DeliverNew, so everything
// published before it bound is legitimately absent and must not be charged as a
// gap.
func (l *seqLedger) observe(key string, seq uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.counts.Delivered++
	cursor, seen := l.lanes[key]
	if !seen {
		l.lanes[key] = seq
		l.counts.Lanes = len(l.lanes)
		return
	}
	if seq <= cursor {
		l.counts.Regressions++
		return
	}
	l.counts.Gaps += int64(seq-cursor) - 1
	l.lanes[key] = seq
}

func (l *seqLedger) snapshot() seqCounts {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.counts
}

// guardStats is the guard's verdict, split by the class the publisher assigned
// so each number answers exactly one question.
type guardStats struct {
	// DupsCaught: a treatment event whose second copy the guard suppressed. This
	// is the guard working.
	DupsCaught int64 `json:"dups_caught"`
	// DupsMissed: a treatment event whose effect ran more than once. This is the
	// double-apply the guard exists to prevent.
	DupsMissed int64 `json:"dups_missed"`
	// DupsUnobserved: a treatment event only one copy of which arrived. Not a
	// guard failure — the lane dropped the other copy — but it must be visible,
	// because it shrinks the sample the other two numbers are drawn from.
	DupsUnobserved int64 `json:"dups_unobserved"`
	// FalsePositives: the guard suppressed an event whose effect had never run.
	// Must be zero. A non-zero value here is worse than a missed duplicate: it is
	// silent data loss dressed as deduplication.
	FalsePositives int64 `json:"false_positives"`
	// ControlCaught: a control event suppressed after having run — a NATURAL
	// redelivery caught. Expected and healthy; separated from DupsCaught so the
	// injected test vector's yield is not inflated by broker redeliveries.
	ControlCaught int64 `json:"control_redeliveries_caught"`
	// ControlDoubleApplied: a control event applied twice. Same defect as
	// DupsMissed, arrived at through a redelivery rather than an injection.
	ControlDoubleApplied int64 `json:"control_double_applied"`
	Applied              int64 `json:"applied"`
	Skipped              int64 `json:"skipped"`
	Events               int64 `json:"events_classified"`
}

func (s *guardStats) add(other guardStats) {
	s.DupsCaught += other.DupsCaught
	s.DupsMissed += other.DupsMissed
	s.DupsUnobserved += other.DupsUnobserved
	s.FalsePositives += other.FalsePositives
	s.ControlCaught += other.ControlCaught
	s.ControlDoubleApplied += other.ControlDoubleApplied
	s.Applied += other.Applied
	s.Skipped += other.Skipped
	s.Events += other.Events
}

// guardEntry is what one event id accumulated inside its window. uint16 because
// nothing legitimate reaches 65535 copies of one event and a saturating counter
// is cheaper than a slice of timestamps.
type guardEntry struct {
	class   eventClass
	applies uint16
	skips   uint16
}

// guardLedger classifies the guard's behaviour per event id.
//
// It deliberately classifies on EVICTION, not on arrival. Two copies of one
// event are in flight on the same pod at the same time under a hundred handler
// routines, and the guard's claim-before ordering means the second copy can
// learn it is a duplicate before the first copy has finished recording that it
// applied. Classifying at arrival would race that window and report healthy
// deduplication as a false positive. Once a window has rotated out, both copies
// have been recorded and the counters are unambiguous.
//
// Memory is bounded by two windows of guard-sampled traffic rather than by the
// run length, which is what lets a five-minute soak at six figures a second keep
// exact per-event accounting.
type guardLedger struct {
	mu       sync.Mutex
	window   time.Duration
	rotateAt time.Time
	current  map[string]*guardEntry
	previous map[string]*guardEntry
	stats    guardStats
	now      func() time.Time
}

// newGuardLedger sizes its window from the injected duplicate delay: an event
// whose two copies straddle a rotation is classified as two separate events, so
// the window has to be several times the widest gap the publisher creates.
func newGuardLedger(window time.Duration, now func() time.Time) *guardLedger {
	if now == nil {
		now = time.Now
	}
	if window <= 0 {
		window = time.Minute
	}
	return &guardLedger{
		window:   window,
		rotateAt: now().Add(window),
		current:  make(map[string]*guardEntry),
		previous: make(map[string]*guardEntry),
		now:      now,
	}
}

// record notes one guard-sampled delivery. applied is whether the guard admitted
// it (the effect ran) or suppressed it.
func (l *guardLedger) record(id string, class eventClass, applied bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rotateLocked()
	entry := l.entryLocked(id, class)
	if applied {
		entry.applies = saturate(entry.applies)
		l.stats.Applied++
		return
	}
	entry.skips = saturate(entry.skips)
	l.stats.Skipped++
}

func saturate(n uint16) uint16 {
	if n == ^uint16(0) {
		return n
	}
	return n + 1
}

// entryLocked finds an id in either live generation, promoting a previous-window
// entry into the current one so an event whose copies straddle a rotation is
// still classified as one event.
func (l *guardLedger) entryLocked(id string, class eventClass) *guardEntry {
	if entry, ok := l.current[id]; ok {
		return entry
	}
	if entry, ok := l.previous[id]; ok {
		delete(l.previous, id)
		l.current[id] = entry
		return entry
	}
	entry := &guardEntry{class: class}
	l.current[id] = entry
	return entry
}

func (l *guardLedger) rotateLocked() {
	if l.now().Before(l.rotateAt) {
		return
	}
	l.classifyLocked(l.previous)
	l.previous = l.current
	l.current = make(map[string]*guardEntry)
	l.rotateAt = l.now().Add(l.window)
}

func (l *guardLedger) classifyLocked(entries map[string]*guardEntry) {
	for _, entry := range entries {
		l.stats.add(classify(*entry))
	}
}

// classify turns one event's counters into a verdict. Written as a pure function
// of the counters so the whole matrix is testable without a broker, a clock or a
// Valkey.
func classify(entry guardEntry) guardStats {
	if entry.applies == 0 {
		// Nothing ever ran. Any suppression here removed an effect outright.
		return guardStats{Events: 1, FalsePositives: int64(entry.skips)}
	}
	stats := guardStats{Events: 1}
	doubled := int64(entry.applies) - 1
	caught := int64(entry.skips)
	if entry.class == classTreatment {
		stats.DupsMissed = doubled
		stats.DupsCaught = caught
		if doubled == 0 && caught == 0 {
			stats.DupsUnobserved = 1
		}
		return stats
	}
	stats.ControlDoubleApplied = doubled
	stats.ControlCaught = caught
	return stats
}

// flush classifies everything still resident. Called once at the end of a run,
// when nothing further can arrive to change a verdict.
func (l *guardLedger) flush() guardStats {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.classifyLocked(l.previous)
	l.classifyLocked(l.current)
	l.previous = make(map[string]*guardEntry)
	l.current = make(map[string]*guardEntry)
	return l.stats
}

// snapshot reports what has been classified so far, for the periodic lines. The
// two live generations are deliberately excluded: an event mid-window has not
// finished happening yet.
func (l *guardLedger) snapshot() guardStats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stats
}

// guardMetrics is the idempotency.Metrics implementation the package has no
// production caller for. It is the measurement, so it counts rather than
// discarding: Duplicate is the guard's own view of how often it fired, and
// FailOpen is how often Valkey was unavailable and the event was admitted
// anyway — the second number is what tells a run with zero caught duplicates
// apart from a run where the store was down the whole time.
type guardMetrics struct {
	duplicates atomic.Int64
	failOpen   atomic.Int64
}

func (m *guardMetrics) Duplicate() { m.duplicates.Add(1) }
func (m *guardMetrics) FailOpen()  { m.failOpen.Add(1) }

func (m *guardMetrics) snapshot() (duplicates, failOpen int64) {
	return m.duplicates.Load(), m.failOpen.Load()
}
