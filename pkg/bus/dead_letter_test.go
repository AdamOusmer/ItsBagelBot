// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// recordingDLQ stands in for the broker at the publish seam. The assertions
// that matter here are exactly what lands on the wire — target subject, the
// original payload verbatim, the metadata headers — so the fake captures
// nats.Msg values rather than ack counts.
type recordingDLQ struct {
	mu   sync.Mutex
	sent []*nats.Msg
	err  error
}

func (r *recordingDLQ) publish(msg *nats.Msg) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.sent = append(r.sent, msg)
	return nil
}

func (r *recordingDLQ) messages() []*nats.Msg {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*nats.Msg(nil), r.sent...)
}

func testDeadLetterer(cfg DeadLetterConfig, rec *recordingDLQ) *deadLetterer {
	d := newDeadLetterer(nil, cfg, nil)
	d.publish = rec.publish
	return d
}

func TestDeadLetterTerminalPublishesOriginalDelivery(t *testing.T) {
	rec := &recordingDLQ{}
	d := testDeadLetterer(DeadLetterConfig{}, rec)

	// The lost banned=true shape: a real payload on a moderation subject,
	// TERMed after its sixth delivery.
	wire := &nats.Msg{
		Subject: "twitch.moderation.event",
		Data:    []byte(`{"banned":true}`),
	}

	if !d.terminal(wire, 6, errRedeliveryBudgetExhausted) {
		t.Fatal("terminal reported no dead-letter; default config must be enabled")
	}

	sent := rec.messages()
	if len(sent) != 1 {
		t.Fatalf("published %d dead-letters, want 1", len(sent))
	}
	msg := sent[0]
	if got := msg.Subject; got != "twitch.moderation.event"+DeadLetterSuffix {
		t.Fatalf("dlq subject = %q, want original subject with suffix", got)
	}
	if string(msg.Data) != string(wire.Data) {
		t.Fatalf("dlq payload = %q, want the original payload verbatim", msg.Data)
	}
	if got := msg.Header.Get(dlqOriginalSubjectHeader); got != wire.Subject {
		t.Fatalf("original-subject header = %q, want %q", got, wire.Subject)
	}
	if got := msg.Header.Get(dlqDeliveryCountHeader); got != "6" {
		t.Fatalf("delivery-count header = %q, want 6", got)
	}
	if got := msg.Header.Get(dlqErrorHeader); got != errRedeliveryBudgetExhausted.Error() {
		t.Fatalf("error header = %q, want the budget-exhausted cause", got)
	}
	stamp := msg.Header.Get(dlqTimestampHeader)
	if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
		t.Fatalf("timestamp header %q is not RFC3339Nano: %v", stamp, err)
	}
	if d.published.Load() != 1 || d.failed.Load() != 0 {
		t.Fatalf("counters published=%d failed=%d, want 1/0", d.published.Load(), d.failed.Load())
	}
}

func TestDeadLetterDisabledEmitsNothing(t *testing.T) {
	rec := &recordingDLQ{}
	d := testDeadLetterer(DeadLetterConfig{Disabled: true}, rec)

	wire := &nats.Msg{Subject: "twitch.moderation.event", Data: []byte(`{"banned":true}`)}
	if d.terminal(wire, 6, errRedeliveryBudgetExhausted) {
		t.Fatal("terminal dead-lettered despite Disabled")
	}
	if len(rec.messages()) != 0 {
		t.Fatal("disabled dead-letterer published to the broker")
	}
	// A nil deadLetterer must behave identically: adapters built directly in
	// tests hold nil and must not panic their way into a different policy.
	var nilDLQ *deadLetterer
	if nilDLQ.enabled() || nilDLQ.terminal(wire, 1, nil) {
		t.Fatal("nil dead-letterer must be the disabled case")
	}
}

func TestDeadLetterPrefixOverridesTarget(t *testing.T) {
	rec := &recordingDLQ{}
	d := testDeadLetterer(DeadLetterConfig{Prefix: "dead"}, rec)

	wire := &nats.Msg{Subject: "discord.ingress.event.standard", Data: []byte("x")}
	d.terminal(wire, 2, errors.New("boom"))

	sent := rec.messages()
	if len(sent) != 1 {
		t.Fatalf("published %d dead-letters, want 1", len(sent))
	}
	if got := sent[0].Subject; got != "dead.discord.ingress.event.standard" {
		t.Fatalf("dlq subject = %q, want prefix-qualified form", got)
	}
	if got := sent[0].Header.Get(dlqErrorHeader); got != "boom" {
		t.Fatalf("error header = %q, want the caller's cause", got)
	}
	if sent[0].Header.Get(dlqDeliveryCountHeader) != "2" {
		t.Fatal("delivery-count header missing")
	}
}

func TestDeadLetterPublishFailureIsCountedNotFatal(t *testing.T) {
	rec := &recordingDLQ{err: errors.New("nats: connection closed")}
	d := testDeadLetterer(DeadLetterConfig{}, rec)

	wire := &nats.Msg{Subject: "s", Data: []byte("x")}
	if d.terminal(wire, 3, errors.New("cause")) {
		t.Fatal("terminal reported success for a failed publish")
	}
	if d.failed.Load() != 1 || d.published.Load() != 0 {
		t.Fatalf("counters published=%d failed=%d, want 0/1", d.published.Load(), d.failed.Load())
	}
}

// The durable subscriber's final-failure path must dead-letter BEFORE the TERM
// lands, because on a work-queue stream TERM deletes the message. The fake
// wire message cannot carry JetStream metadata (that needs nats.go's own
// subscription binding), so the resolved plan is applied directly — the same
// split production code makes in redeliveryPlan.
func TestDurableSubscriberTermPathDeadLetters(t *testing.T) {
	rec := &recordingDLQ{}
	s := newConcurrentDurableSubscriber(concurrentSubscriberConfig{
		stream: OutgressStream.Name,
		delay:  maxRetryDelay{delay: time.Second, max: 6},
		dlq:    DeadLetterConfig{},
	})
	s.dlq.publish = rec.publish

	s.applyNak(&nats.Msg{Subject: "twitch.moderation.event", Data: []byte(`{"banned":true}`)},
		terminateDelivery, 6)
	if len(rec.messages()) != 1 {
		t.Fatalf("budget TERM produced %d dead-letters, want 1", len(rec.messages()))
	}

	// A mid-budget NAK must stay a plain paced redelivery with no dead-letter.
	s.applyNak(&nats.Msg{Subject: "twitch.moderation.event"}, 3*time.Second, 2)
	if len(rec.messages()) != 1 {
		t.Fatalf("paced NAK dead-lettered; want redelivery only (%d)", len(rec.messages()))
	}

	// The malformed-delivery TERM path dead-letters too.
	s.terminateMalformed(&nats.Msg{Subject: "twitch.ingress.event.standard", Data: []byte("junk")},
		"twitch.ingress.event.standard", errors.New("bus: multiple values in NATS header"))
	if len(rec.messages()) != 2 {
		t.Fatalf("malformed TERM produced %d total dead-letters, want 2", len(rec.messages()))
	}
	if got := rec.messages()[1].Header.Get(dlqOriginalSubjectHeader); got != "twitch.ingress.event.standard" {
		t.Fatalf("malformed dlq original subject = %q", got)
	}
}
