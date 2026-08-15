package bus

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestMessageAcknowledgementIsIdempotentAndExclusive(t *testing.T) {
	msg := NewMessage("id", nil)
	if !msg.Ack() || !msg.Ack() {
		t.Fatal("Ack must be idempotent")
	}
	if msg.Nack() {
		t.Fatal("Nack must lose after Ack")
	}
	select {
	case <-msg.Acked():
	default:
		t.Fatal("Acked channel was not closed")
	}
	select {
	case <-msg.Nacked():
		t.Fatal("Nacked channel closed after Ack")
	default:
	}
}

func TestZeroValueMessageInitializesAcknowledgementSignals(t *testing.T) {
	var acked Message
	if !acked.Ack() {
		t.Fatal("zero-value message could not ack")
	}
	assertSignalState(t, acked.Acked(), true, "acked")
	assertSignalState(t, acked.Nacked(), false, "nacked after ack")

	var nacked Message
	if !nacked.Nack() {
		t.Fatal("zero-value message could not nack")
	}
	assertSignalState(t, nacked.Nacked(), true, "nacked")
	assertSignalState(t, nacked.Acked(), false, "acked after nack")
}

// TestNewMessageDoesNotPreallocateSignals is a cost assertion, not a behaviour
// one. The receipt-level lane adapters resolve through the callback and never
// read either signal, so allocating both per delivery was two channels of pure
// garbage at lane rate.
func TestNewMessageDoesNotPreallocateSignals(t *testing.T) {
	msg := NewMessage("id", nil)
	if msg.ack != nil || msg.nack != nil {
		t.Fatal("acknowledgement signals were allocated before anyone asked for one")
	}
	// A caller that does ask still gets the pair in the state the message is in.
	assertSignalState(t, msg.Acked(), false, "acked")
	assertSignalState(t, msg.Nacked(), false, "nacked")
	if !msg.Ack() {
		t.Fatal("a message with lazily created signals could not ack")
	}
	assertSignalState(t, msg.Acked(), true, "acked after ack")
	assertSignalState(t, msg.Nacked(), false, "nacked after ack")
}

// TestResolveHandlerFiresOnceOnTheWinningResult is the lane adapters' contract.
// They replaced a per-message goroutine with this callback, so a second
// invocation schedules a second retry and a missing one leaks the adapter's
// pending count until shutdown times the drain out.
func TestResolveHandlerFiresOnceOnTheWinningResult(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		resolve func(*Message) bool
		acked   bool
	}{
		{name: "ack", resolve: (*Message).Ack, acked: true},
		{name: "nack", resolve: (*Message).Nack, acked: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var calls, ackedCalls atomic.Int64
			msg := NewMessage("id", nil)
			msg.setResolveHandler(func(acked bool) {
				calls.Add(1)
				if acked {
					ackedCalls.Add(1)
				}
			})

			if !testCase.resolve(msg) {
				t.Fatal("the first resolution must win")
			}
			// Every later call, winning side or losing side, is a no-op.
			msg.Ack()
			msg.Nack()

			if calls.Load() != 1 {
				t.Fatalf("resolve handler ran %d times, want exactly one", calls.Load())
			}
			if wasAcked := ackedCalls.Load() == 1; wasAcked != testCase.acked {
				t.Fatalf("handler was told acked=%v, want %v", wasAcked, testCase.acked)
			}
			assertSignalState(t, msg.Acked(), testCase.acked, "acked")
			assertSignalState(t, msg.Nacked(), !testCase.acked, "nacked")
		})
	}
}

// TestResolveHandlerRunsOutsideTheMessageLock is what makes the callback safe to
// do real work in: the lane adapters publish a retry schedule from it. Touching
// the message from inside is the cheap proof — Ack and Nack hold a plain
// sync.Mutex, so a callback invoked under it would deadlock here instead of
// returning, and it also shows the transition is already committed when the
// callback sees it.
func TestResolveHandlerRunsOutsideTheMessageLock(t *testing.T) {
	msg := NewMessage("id", nil)
	sawResolved := make(chan bool, 1)
	msg.setResolveHandler(func(bool) {
		select {
		case <-msg.Acked():
			sawResolved <- true
		default:
			sawResolved <- false
		}
	})

	done := make(chan struct{})
	go func() { defer close(done); msg.Ack() }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the resolve handler was invoked while the message lock was held")
	}
	if !<-sawResolved {
		t.Fatal("the resolve handler ran before the acknowledgement was committed")
	}
}

func assertSignalState(t *testing.T, signal <-chan struct{}, wantClosed bool, name string) {
	t.Helper()
	select {
	case <-signal:
		if !wantClosed {
			t.Fatalf("%s signal closed", name)
		}
	default:
		if wantClosed {
			t.Fatalf("%s signal remained open", name)
		}
	}
}

func TestMessageFromNATSUsesFleetIdentityAndCopiesMetadata(t *testing.T) {
	wire := nats.NewMsg("data.test")
	wire.Data = []byte("payload")
	wire.Header.Set(MessageIDHeader, "fleet-id")
	wire.Header.Set(legacyMessageIDHeader, "legacy-id")
	wire.Header.Set(nats.MsgIdHdr, "broker-dedup-id")
	wire.Header.Set("Traceparent", "trace-id")

	msg, err := messageFromNATS(wire)
	if err != nil {
		t.Fatal(err)
	}
	if msg.UUID != "fleet-id" {
		t.Fatalf("message id = %q, want fleet-id", msg.UUID)
	}
	if msg.Metadata.Get("Traceparent") != "trace-id" {
		t.Fatalf("trace metadata = %q", msg.Metadata.Get("Traceparent"))
	}
	if _, ok := msg.Metadata[MessageIDHeader]; ok {
		t.Fatal("fleet identity leaked into application metadata")
	}
	if _, ok := msg.Metadata[nats.MsgIdHdr]; ok {
		t.Fatal("broker dedup identity leaked into application metadata")
	}
}

func TestMessageFromNATSAcceptsLegacyIdentityDuringRollout(t *testing.T) {
	wire := nats.NewMsg("data.test")
	wire.Header.Set(legacyMessageIDHeader, "legacy-id")
	msg, err := messageFromNATS(wire)
	if err != nil {
		t.Fatal(err)
	}
	if msg.UUID != "legacy-id" {
		t.Fatalf("message id = %q, want legacy-id", msg.UUID)
	}
}

func TestMessageFromNATSUsesStableJetStreamSequenceFallback(t *testing.T) {
	wire := nats.NewMsg("data.test")
	wire.Reply = "$JS.ACK.STREAM.CONSUMER.1.42.7.1000000000.0"
	wire.Sub = &nats.Subscription{}
	msg, err := messageFromNATS(wire)
	if err != nil {
		t.Fatal(err)
	}
	if msg.UUID != "js::STREAM:42" {
		t.Fatalf("message id = %q, want stable stream sequence", msg.UUID)
	}
}

func TestMessageFromNATSRejectsMultiValueMetadata(t *testing.T) {
	wire := nats.NewMsg("data.test")
	wire.Header["Traceparent"] = []string{"one", "two"}
	if _, err := messageFromNATS(wire); err == nil {
		t.Fatal("multi-value application metadata must be rejected")
	}
}

func TestMaxRetryDelayTerminatesFinalDelivery(t *testing.T) {
	delay := newMaxRetryDelay(3, 3)
	if got := delay.WaitTime(2); got != 3 {
		t.Fatalf("retry delay = %v, want 3", got)
	}
	if got := delay.WaitTime(3); got != terminateDelivery {
		t.Fatalf("final delivery = %v, want terminate signal", got)
	}
}
