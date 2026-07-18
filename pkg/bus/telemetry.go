package bus

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
)

const (
	messagingSystemAttribute      = "messaging.system"
	messagingOperationAttribute   = "messaging.operation"
	messagingDestinationAttribute = "messaging.destination"
	resultAttribute               = "result"
)

// startMessagingSegment creates a fixed-name span and puts the configured
// destination in an attribute. Keeping subjects out of span names prevents an
// accidentally ID-expanded subject from creating unbounded metric names.
func startMessagingSegment(ctx context.Context, name, operation, destination string) *newrelic.Segment {
	txn := newrelic.FromContext(ctx)
	if txn == nil {
		return nil
	}
	segment := txn.StartSegment(name)
	segment.AddAttribute(messagingSystemAttribute, "nats")
	segment.AddAttribute(messagingOperationAttribute, operation)
	segment.AddAttribute(messagingDestinationAttribute, normalizedDestination(destination))
	return segment
}

func endMessagingSegment(segment *newrelic.Segment, err error) {
	if segment == nil {
		return
	}
	segment.AddAttribute(resultAttribute, messagingResult(err))
	segment.End()
}

func messagingResult(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, nats.ErrTimeout):
		return "timeout"
	default:
		return "error"
	}
}

func insertTraceHeaders(ctx context.Context, msg *nats.Msg) {
	txn := newrelic.FromContext(ctx)
	if txn == nil {
		return
	}
	headers := http.Header{}
	txn.InsertDistributedTraceHeaders(headers)
	for key := range headers {
		msg.Header.Set(key, headers.Get(key))
	}
}

func acceptTraceHeaders(txn *newrelic.Transaction, headers nats.Header) {
	if txn == nil || len(headers) == 0 {
		return
	}
	httpHeaders := make(http.Header, len(headers))
	for key, values := range headers {
		for _, value := range values {
			httpHeaders.Add(key, value)
		}
	}
	txn.AcceptDistributedTraceHeaders(newrelic.TransportQueue, httpHeaders)
}

func addMessagingTransactionAttributes(txn *newrelic.Transaction, operation, destination string) {
	if txn == nil {
		return
	}
	txn.AddAttribute(messagingSystemAttribute, "nats")
	txn.AddAttribute(messagingOperationAttribute, operation)
	txn.AddAttribute(messagingDestinationAttribute, normalizedDestination(destination))
}

// normalizedDestination intentionally reports a configured family rather than
// the raw wire subject. Raw subjects are still available in correlated logs;
// keeping them out of APM facets prevents IDs or tenant tokens from creating
// unbounded cardinality.
func normalizedDestination(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) >= 3 && parts[0] == "bagel" && parts[1] == "rpc" {
		switch parts[2] {
		case "admin", "broadcaster", "commands", "dashboard", "delegation", "gateway",
			"health", "ingress", "internal", "loyalty", "modules", "notifications",
			"outgress", "projector", "transactions", "users":
			return "bagel.rpc." + parts[2]
		default:
			return "bagel.rpc.other"
		}
	}
	if len(parts) == 4 && strings.Join(parts[:3], ".") == "twitch.ingress.event" {
		switch parts[3] {
		case "premium", "standard", "stream":
			return subject
		default:
			return "twitch.ingress.event.other"
		}
	}
	if len(parts) == 3 && parts[0] == "twitch" && parts[1] == "outgress" {
		switch parts[2] {
		case "premium", "standard", "system":
			return subject
		default:
			return "twitch.outgress.other"
		}
	}
	if strings.HasPrefix(subject, "bagel.cache.invalidate") {
		return "bagel.cache.invalidate"
	}
	return "other"
}
