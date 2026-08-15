package bus

import (
	"context"
	"errors"
	"os"
	"strings"
	"unicode"

	"github.com/nats-io/nats.go"
)

const rpcNodeToken = "node"

type rpcSubject string
type rpcNodeName string

// rpcSubscriptionSubjects returns the stable HA subject followed by this pod's
// node-local subject. Local development and invalid node names retain the
// generic subject only.
func rpcSubscriptionSubjects(subject rpcSubject) []string {
	return rpcSubjectsForNode(subject, rpcNodeName(os.Getenv("NODE_NAME")))
}

func rpcRequestSubjects(subject rpcSubject) []string {
	subjects := rpcSubscriptionSubjects(subject)
	if len(subjects) == 1 {
		return subjects
	}
	return []string{subjects[1], subjects[0]}
}

func rpcSubjectsForNode(subject rpcSubject, node rpcNodeName) []string {
	if !validSubjectToken(node) {
		return []string{string(subject)}
	}
	return []string{string(subject), string(subject) + "." + rpcNodeToken + "." + string(node)}
}

func validSubjectToken(token rpcNodeName) bool {
	value := string(token)
	return value != "" && !strings.ContainsAny(value, ".*> ") &&
		!strings.ContainsFunc(value, unicode.IsSpace)
}

// RequestWithContext targets the same-node responder first and falls back to
// the generic queue only when NATS proves that no local responder exists.
func RequestWithContext(ctx context.Context, nc *nats.Conn, subject string, data []byte) (*nats.Msg, error) {
	return requestLocalFirst(rpcRequestSubjects(rpcSubject(subject)), func(routedSubject string) (*nats.Msg, error) {
		return nc.RequestWithContext(ctx, routedSubject, data)
	})
}

// RequestMsgWithContext is the header-preserving form used by traced RPCs. It
// does not retry timeouts or connection errors because the request may already
// have executed and replaying a mutation could apply it twice.
func RequestMsgWithContext(ctx context.Context, nc *nats.Conn, msg *nats.Msg) (*nats.Msg, error) {
	return requestLocalFirst(rpcRequestSubjects(rpcSubject(msg.Subject)), func(routedSubject string) (*nats.Msg, error) {
		routed := nats.NewMsg(routedSubject)
		routed.Data = msg.Data
		routed.Header = msg.Header
		return nc.RequestMsgWithContext(ctx, routed)
	})
}

func requestLocalFirst(subjects []string, request func(string) (*nats.Msg, error)) (*nats.Msg, error) {
	for i, subject := range subjects {
		msg, err := request(subject)
		if err == nil {
			return msg, nil
		}
		if i == len(subjects)-1 || !errors.Is(err, nats.ErrNoResponders) {
			return nil, err
		}
	}
	return nil, nats.ErrNoResponders
}

// QueueSubscribeRPC registers the generic HA subject and the node-qualified
// local subject with the same handler.
//
// The handler runs INLINE on nats.go's per-subscription delivery goroutine, so
// deliveries on a subscription are serial and a slow handler blocks the requests
// behind it. That is left as-is on purpose rather than fixed for everyone at
// once: handlers on this path read-then-write shared state (outgress
// channel.set, users ApplyBilling), and the database services cap themselves at
// DB_MAX_OPEN_CONNS=4, so added concurrency would buy lost updates and
// gate-blocked queries instead of throughput. Registrations that are safe for it
// opt in with QueueSubscribeRPCConcurrent.
func QueueSubscribeRPC(nc *nats.Conn, subject, queueGroup string, handler nats.MsgHandler) error {
	_, err := subscribeRPCSubjects(nc, RPCSubscription{Subject: subject, QueueGroup: queueGroup}, handler)
	return err
}

// RPCSubscription is one endpoint registration: the subject it answers, the
// queue group it shares with its replicas, and the sizing of the fleet that runs
// it. The three travel together as one argument because the two strings are
// interchangeable at a call site and a transposed pair registers a working
// subscription on the wrong subject.
type RPCSubscription struct {
	Subject    string
	QueueGroup string
	Policy     RPCPoolPolicy
}

// QueueSubscribeRPCConcurrent is QueueSubscribeRPC with the handler moved off
// the delivery goroutine onto a bounded fleet of persistent workers, so a slow
// request no longer holds the ones behind it. See RPCPool for the failure it
// removes and for what a handler must be safe against — every handler moved here
// MUST be safe to run concurrently with itself. It is opt-in for exactly that
// reason: the rest of the fleet's RPC surface is not, and those are separate
// fixes (see QueueSubscribeRPC).
//
// The returned pool is registered with DrainRPCHandlers, so a service can drain
// every endpoint it registered with one call and never has to hold the handle.
//
// Both subjects feed the SAME pool. They are two subscriptions to one logical
// endpoint — the generic HA subject and this pod's node-local subject, which
// requestLocalFirst prefers — so giving each its own fleet would double the
// concurrency budget the pool exists to bound.
func QueueSubscribeRPCConcurrent(nc *nats.Conn, sub RPCSubscription, handler nats.MsgHandler) (*RPCPool, error) {
	pool := newRPCPool(sub.Policy)
	subscriptions, err := subscribeRPCSubjects(nc, sub, pool.callback(handler))
	if err != nil {
		return nil, err
	}
	pool.adopt(subscriptions)
	registerRPCPool(pool)
	return pool, nil
}

// subscribeRPCSubjects registers callback on every subject the RPC locality
// scheme serves. A failure returns the subjects that did register, so the caller
// still owns them; every caller in the fleet treats a subscribe error as fatal
// and the process exits, which releases them anyway.
func subscribeRPCSubjects(nc *nats.Conn, sub RPCSubscription, callback nats.MsgHandler) ([]*nats.Subscription, error) {
	var subscriptions []*nats.Subscription
	for _, routedSubject := range rpcSubscriptionSubjects(rpcSubject(sub.Subject)) {
		registered, err := nc.QueueSubscribe(routedSubject, sub.QueueGroup, callback)
		if err != nil {
			return subscriptions, err
		}
		subscriptions = append(subscriptions, registered)
	}
	return subscriptions, nil
}
