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
// local subject with the same handler. A partial registration is rolled back.
func QueueSubscribeRPC(nc *nats.Conn, subject, queueGroup string, handler nats.MsgHandler) error {
	var subscriptions []*nats.Subscription
	for _, routedSubject := range rpcSubscriptionSubjects(rpcSubject(subject)) {
		sub, err := nc.QueueSubscribe(routedSubject, queueGroup, handler)
		if err != nil {
			for _, registered := range subscriptions {
				_ = registered.Unsubscribe()
			}
			return err
		}
		subscriptions = append(subscriptions, sub)
	}
	return nil
}
