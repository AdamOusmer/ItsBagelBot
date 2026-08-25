// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"errors"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// Dead-lettering is the answer to one measured loss shape: a delivery whose
// redelivery budget is spent disappears. On a work-queue stream TERM does not
// park the message for inspection — it DELETES it — so a payload that poisoned
// six handlers (or a transient outage that outlasted the budget) leaves no
// trace beyond a log line, and nobody can answer "what exactly did we lose".
// The dead letter is that trace: the original payload verbatim on a derived
// subject, with enough headers to reconstruct what happened and where it came
// from, published BEFORE the TERM lands.
//
// Publishing is best-effort by design: an outage strong enough to break the
// DLQ publish is the same outage that exhausted the budget in the first place,
// and a failed publish must not replace one lost message with a crash loop.
// Counters make the failure visible instead.
const (
	// DeadLetterSuffix qualifies the default dead-letter target: the original
	// subject plus this suffix ("twitch.moderation.event" ->
	// "twitch.moderation.event.dlq"), so each subject's dead letters land
	// under a name operators can wildcard independently.
	DeadLetterSuffix = ".dlq"
)

// DeadLetter headers carry the context the raw payload cannot.
const (
	dlqOriginalSubjectHeader = "Bagelbot-DLQ-Original-Subject"
	dlqDeliveryCountHeader   = "Bagelbot-DLQ-Delivery-Count"
	dlqErrorHeader           = "Bagelbot-DLQ-Error"
	dlqTimestampHeader       = "Bagelbot-DLQ-Timestamp" // RFC3339Nano
)

// errRedeliveryBudgetExhausted is the canonical cause stamped on deliveries
// TERMed at the end of their budget; other terminal paths stamp their own.
var errRedeliveryBudgetExhausted = errors.New("bus: redelivery budget exhausted")

// DeadLetterConfig tunes one subscriber's dead-letter behaviour. Zero values
// are meaningful: enabled by default, target = original + DeadLetterSuffix —
// the posture every lane consumer should have unless it says otherwise.
type DeadLetterConfig struct {
	// Disabled turns dead-lettering off entirely (the zero-value deadLetterer
	// behaves identically). For lanes whose subjects are pure telemetry, where
	// the dead letters would only re-create the noise the MaxAge already bounds.
	Disabled bool
	// Prefix overrides the target derivation with "<Prefix>.<subject>", for
	// deployments that collect dead letters under one namespace instead of
	// alongside their source subjects.
	Prefix string
}

// deadLetterer publishes terminal deliveries. It is nil-safe end to end: a nil
// receiver IS the disabled configuration, which lets adapters built directly in
// tests hold nil without branching.
type deadLetterer struct {
	nc  *nats.Conn
	cfg DeadLetterConfig
	log *zap.Logger

	// publish is the wire seam; production leaves it nil and nc.Publish is
	// used, tests capture the messages instead. Read per call, not bound at
	// construction, so a test can swap it after the subscriber is built.
	publish func(*nats.Msg) error

	published atomic.Int64
	failed    atomic.Int64
}

func newDeadLetterer(nc *nats.Conn, cfg DeadLetterConfig, log *zap.Logger) *deadLetterer {
	if log == nil {
		log = zap.NewNop()
	}
	return &deadLetterer{nc: nc, cfg: cfg, log: log}
}

func (d *deadLetterer) enabled() bool {
	return d != nil && !d.cfg.Disabled
}

func (d *deadLetterer) target(subject string) string {
	if d.cfg.Prefix != "" {
		return d.cfg.Prefix + "." + subject
	}
	return subject + DeadLetterSuffix
}

// terminal dead-letters one delivery whose redelivery budget is spent,
// reporting whether it was published. It must be called BEFORE msg.Term(): on
// work-queue retention Term deletes the message, and a publish after it would
// describe a payload that no longer exists anywhere.
func (d *deadLetterer) terminal(msg *nats.Msg, delivery uint64, cause error) bool {
	return d.emit(msg.Subject, msg, delivery, cause)
}

// emit is terminal with an explicit original subject, for terminal paths whose
// routing subject differs from the wire subject (a malformed delivery is
// terminated against the subscription's filter subject).
func (d *deadLetterer) emit(originalSubject string, wire *nats.Msg, delivery uint64, cause error) bool {
	if !d.enabled() {
		return false
	}

	out := nats.NewMsg(d.target(originalSubject))
	out.Data = wire.Data // verbatim: the point is to keep the poison intact
	out.Header.Set(dlqOriginalSubjectHeader, originalSubject)
	out.Header.Set(dlqDeliveryCountHeader, strconv.FormatUint(delivery, 10))
	if cause != nil {
		out.Header.Set(dlqErrorHeader, cause.Error())
	}
	out.Header.Set(dlqTimestampHeader, time.Now().Format(time.RFC3339Nano))

	publish := d.publish
	if publish == nil {
		publish = func(m *nats.Msg) error { return d.nc.Publish(m.Subject, m.Data) }
	}
	if err := publish(out); err != nil {
		d.failed.Add(1)
		d.log.Warn("dead-letter publish failed",
			zap.String("original_subject", originalSubject),
			zap.Uint64("delivery", delivery), zap.Error(err))
		return false
	}
	d.published.Add(1)
	return true
}
