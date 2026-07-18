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
	rpcDestinationPrefix          = "bagel.rpc."
	ingressDestinationPrefix      = "twitch.ingress.event."
	outgressDestinationPrefix     = "twitch.outgress."
	cacheDestinationPrefix        = "bagel.cache.invalidate"
)

var rpcDestinations = map[string]struct{}{
	"admin": {}, "broadcaster": {}, "commands": {}, "dashboard": {},
	"delegation": {}, "gateway": {}, "health": {}, "ingress": {},
	"internal": {}, "loyalty": {}, "modules": {}, "notifications": {},
	"outgress": {}, "projector": {}, "transactions": {}, "users": {},
}

type destinationFamily struct {
	fallback string
	allowed  map[string]struct{}
}

type messagingSpan struct {
	name        string
	operation   string
	destination string
}

type messagingAttributes struct {
	operation   string
	destination string
}

var ingressDestinations = destinationFamily{
	fallback: ingressDestinationPrefix + "other",
	allowed: map[string]struct{}{
		ingressDestinationPrefix + "premium":  {},
		ingressDestinationPrefix + "standard": {},
		ingressDestinationPrefix + "stream":   {},
	},
}

var outgressDestinations = destinationFamily{
	fallback: outgressDestinationPrefix + "other",
	allowed: map[string]struct{}{
		outgressDestinationPrefix + "premium":  {},
		outgressDestinationPrefix + "standard": {},
		outgressDestinationPrefix + "system":   {},
	},
}

// startMessagingSegment creates a fixed-name span and puts the configured
// destination in an attribute. Keeping subjects out of span names prevents an
// accidentally ID-expanded subject from creating unbounded metric names.
func startMessagingSegment(ctx context.Context, span messagingSpan) *newrelic.Segment {
	txn := newrelic.FromContext(ctx)
	if txn == nil {
		return nil
	}
	segment := txn.StartSegment(span.name)
	segment.AddAttribute(messagingSystemAttribute, "nats")
	segment.AddAttribute(messagingOperationAttribute, span.operation)
	segment.AddAttribute(messagingDestinationAttribute, normalizedDestination(span.destination))
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

func addMessagingTransactionAttributes(txn *newrelic.Transaction, attributes messagingAttributes) {
	if txn == nil {
		return
	}
	txn.AddAttribute(messagingSystemAttribute, "nats")
	txn.AddAttribute(messagingOperationAttribute, attributes.operation)
	txn.AddAttribute(messagingDestinationAttribute, normalizedDestination(attributes.destination))
}

// normalizedDestination intentionally reports a configured family rather than
// the raw wire subject. Raw subjects are still available in correlated logs;
// keeping them out of APM facets prevents IDs or tenant tokens from creating
// unbounded cardinality.
func normalizedDestination(subject string) string {
	switch {
	case strings.HasPrefix(subject, rpcDestinationPrefix):
		return normalizedRPCDestination(subject)
	case strings.HasPrefix(subject, ingressDestinationPrefix):
		return ingressDestinations.normalize(subject)
	case strings.HasPrefix(subject, outgressDestinationPrefix):
		return outgressDestinations.normalize(subject)
	case strings.HasPrefix(subject, cacheDestinationPrefix):
		return cacheDestinationPrefix
	default:
		return "other"
	}
}

func normalizedRPCDestination(subject string) string {
	remainder := strings.TrimPrefix(subject, rpcDestinationPrefix)
	service, _, _ := strings.Cut(remainder, ".")
	if _, ok := rpcDestinations[service]; ok {
		return rpcDestinationPrefix + service
	}
	return rpcDestinationPrefix + "other"
}

func (family destinationFamily) normalize(subject string) string {
	if _, ok := family.allowed[subject]; ok {
		return subject
	}
	return family.fallback
}
