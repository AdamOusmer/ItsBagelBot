// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
)

const (
	flowControlHeartbeat = time.Second
	flowMaxAckPending    = 20_000

	flowProvisionTimeout = 5 * time.Second
	laneReplaceTimeout   = 20 * time.Second

	// Must stay far longer than a reconnect, or a returning pod loses its ack floor.
	flowInactiveThreshold = 5 * time.Minute
)

type laneBinding struct {
	stream   string
	subject  string
	consumer string
}

// No DeliverGroup: one member's cumulative flow ack would cover work in flight on another.
func flowConsumerConfig(lane laneBinding) jsapi.ConsumerConfig {
	return jsapi.ConsumerConfig{
		Name:              lane.consumer,
		Durable:           lane.consumer,
		Description:       "ItsBagelBot R3 flow-controlled ingress lane consumer",
		DeliverPolicy:     jsapi.DeliverNewPolicy,
		AckPolicy:         jsapi.AckFlowControlPolicy,
		MaxDeliver:        -1,
		FilterSubject:     lane.subject,
		ReplayPolicy:      jsapi.ReplayInstantPolicy,
		MaxAckPending:     flowMaxAckPending,
		DeliverSubject:    "_INBOX.BAGEL." + subjectToken(lane.consumer),
		FlowControl:       true,
		IdleHeartbeat:     flowControlHeartbeat,
		InactiveThreshold: flowInactiveThreshold,
		Replicas:          1,
		MemoryStorage:     true,
		Metadata:          map[string]string{managedConsumerMetadata: "true"},
	}
}

func flowConsumerName(group, subject string) string {
	return durableName(group, subject) + "_" + podIdentity()
}

func podIdentity() string {
	name := env.Get("POD_NAME", env.Get("HOSTNAME", ""))
	if name == "" {
		name, _ = os.Hostname()
	}
	if name == "" {
		name = nuid.Next()
	}
	return consumerToken(name)
}

func consumerToken(name string) string {
	token := strings.Map(func(char rune) rune {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z':
			return char
		case char >= '0' && char <= '9', char == '-', char == '_':
			return char
		default:
			return '_'
		}
	}, name)
	if len(token) > 48 {
		token = token[:48]
	}
	return token
}

func recoveryFlowConsumerConfig(lane laneBinding, position flowPosition) jsapi.ConsumerConfig {
	desired := flowConsumerConfig(lane)
	if position.stream == 0 {
		return desired
	}
	desired.DeliverPolicy = jsapi.DeliverByStartSequencePolicy
	desired.OptStartSeq = position.stream + 1
	return desired
}

// Only these rejections may delete a live consumer; deleting on any other resets every pod.
var immutableConsumerFieldErrors = []string{
	"ack policy can not be updated",
	"storage type can not be updated",
	"flow control can not be updated",
	"heart beats can not be updated",
	"can not update push consumer to pull based",
	"can not update pull consumer to push based",
}

func requiresConsumerReplacement(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, immutable := range immutableConsumerFieldErrors {
		if strings.Contains(message, immutable) {
			return true
		}
	}
	return false
}

func ensureFlowConsumer(nc *nats.Conn, lane laneBinding, desired jsapi.ConsumerConfig) (string, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return "", fmt.Errorf("bus: modern jetstream context: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), flowProvisionTimeout)
	defer cancel()

	info, err := flowConsumerInfo(ctx, js, lane)
	if err != nil {
		return "", err
	}
	if info == nil {
		_, err = js.CreateOrUpdatePushConsumer(ctx, lane.stream, desired)
		return desired.DeliverSubject, err
	}

	// Immutable: echo the server's values or every boot is rejected.
	desired.DeliverSubject = info.Config.DeliverSubject
	desired.DeliverPolicy = info.Config.DeliverPolicy
	desired.OptStartSeq = info.Config.OptStartSeq
	if _, err = js.UpdatePushConsumer(ctx, lane.stream, desired); err == nil {
		return desired.DeliverSubject, nil
	}
	if !requiresConsumerReplacement(err) {
		return "", fmt.Errorf("bus: update flow consumer %q: %w", desired.Name, err)
	}

	carryLaneAckFloor(&desired, info)
	return desired.DeliverSubject, replaceFlowConsumer(js, lane, desired, err)
}

func flowConsumerInfo(ctx context.Context, js jsapi.JetStream, lane laneBinding) (*jsapi.ConsumerInfo, error) {
	consumer, err := js.PushConsumer(ctx, lane.stream, lane.consumer)
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := consumer.Info(ctx)
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil, nil
	}
	return info, err
}

func replaceFlowConsumer(
	js jsapi.JetStream,
	lane laneBinding,
	desired jsapi.ConsumerConfig,
	cause error,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), laneReplaceTimeout)
	defer cancel()

	if err := js.DeleteConsumer(ctx, lane.stream, desired.Name); err != nil &&
		!errors.Is(err, jsapi.ErrConsumerNotFound) {
		return fmt.Errorf("bus: update flow consumer %q: %w (replace failed: %v)", desired.Name, cause, err)
	}
	if _, err := js.CreateOrUpdatePushConsumer(ctx, lane.stream, desired); err != nil {
		return fmt.Errorf("bus: recreate flow consumer %q: %w", desired.Name, err)
	}
	return nil
}

// Never fall back to DeliverAll: it re-executes every retained chat command.
func carryLaneAckFloor(desired *jsapi.ConsumerConfig, info *jsapi.ConsumerInfo) {
	if info == nil || info.AckFloor.Stream == 0 {
		desired.DeliverPolicy = jsapi.DeliverNewPolicy
		desired.OptStartSeq = 0
		return
	}
	desired.DeliverPolicy = jsapi.DeliverByStartSequencePolicy
	desired.OptStartSeq = info.AckFloor.Stream + 1
}
