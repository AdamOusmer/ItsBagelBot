// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

func RequestWithContext(ctx context.Context, nc *nats.Conn, subject string, data []byte) (*nats.Msg, error) {
	return requestLocalFirst(rpcRequestSubjects(rpcSubject(subject)), func(routedSubject string) (*nats.Msg, error) {
		return nc.RequestWithContext(ctx, routedSubject, data)
	})
}

// Never retry timeouts: the request may have executed, and a replayed mutation applies twice.
func RequestMsgWithContext(ctx context.Context, nc *nats.Conn, msg *nats.Msg) (*nats.Msg, error) {
	routed := &nats.Msg{Header: msg.Header, Data: msg.Data}
	return requestLocalFirst(rpcRequestSubjects(rpcSubject(msg.Subject)), func(routedSubject string) (*nats.Msg, error) {
		routed.Subject = routedSubject
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

func QueueSubscribeRPC(nc *nats.Conn, subject, queueGroup string, handler nats.MsgHandler) error {
	_, err := subscribeRPCSubjects(nc, RPCSubscription{Subject: subject, QueueGroup: queueGroup}, handler)
	return err
}

type RPCSubscription struct {
	Subject    string
	QueueGroup string
	Policy     RPCPoolPolicy
}

// handler must be safe to run concurrently with itself.
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
