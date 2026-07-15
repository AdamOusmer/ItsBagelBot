package bus

import (
	"testing"

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
