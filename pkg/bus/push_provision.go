// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

func pushConsumerName(group, subject string) string {
	return durableName(group, subject) + "_v3"
}

func pushConsumerConfig(cfg flowLaneConfig) jsapi.ConsumerConfig {
	name := pushConsumerName(cfg.group, cfg.subject)
	desired := pullConsumerConfig(cfg.subject, name)
	desired.Description = "ItsBagelBot shared-durable push lane consumer"
	desired.MaxAckPending = defaultPullMaxAckPending
	desired.DeliverSubject = "_INBOX.LANE." + subjectToken(name)
	desired.DeliverGroup = cfg.group
	desired.FlowControl = true
	desired.IdleHeartbeat = flowControlHeartbeat
	desired.InactiveThreshold = 0
	desired.Metadata[consumerContractMetadata] = "3"
	return desired
}

type hotDurable string

type hotLaneBinding struct {
	*pullSubscriber
	js    jsapi.JetStream
	other hotDurable
	claim jsapi.ConsumerConfig

	hotLaneState
}

func newHotLane(s *pullSubscriber, cfg flowLaneConfig) (*hotLaneBinding, error) {
	js, err := jsapi.NewWithDomain(s.nc, JSDomain())
	if err != nil {
		return nil, err
	}
	base := durableName(cfg.group, cfg.subject)
	claim := pullConsumerConfig(cfg.subject, base+"_handover")
	claim.DeliverPolicy = jsapi.DeliverNewPolicy
	claim.InactiveThreshold = 3 * pullProvisionTimeout
	return &hotLaneBinding{
		pullSubscriber: s, js: js, other: otherModeDurable(hotDurable(s.name)), claim: claim,
		hotLaneState: hotLaneState{alignment: newLaneAlignment()},
	}, nil
}

func otherModeDurable(active hotDurable) hotDurable {
	name := string(active)
	if strings.HasSuffix(name, "_v3") {
		return hotDurable(strings.TrimSuffix(name, "_v3") + "_v2")
	}
	return hotDurable(strings.TrimSuffix(name, "_v2") + "_v3")
}

type hotConsumerResponse struct {
	jsapi.ConsumerInfo
	Error *jsapi.APIError `json:"error"`
}

func (h *hotLaneBinding) info(ctx context.Context, name hotDurable) (*jsapi.ConsumerInfo, error) {
	var response hotConsumerResponse
	subject := h.consumerAPISubject("INFO", name)
	if err := laneRequest(ctx, h.nc, laneRequestArgs{subject: subject}, &response); err != nil {
		return nil, err
	}
	if response.Error == nil {
		return &response.ConsumerInfo, nil
	}
	if errors.Is(response.Error, jsapi.ErrConsumerNotFound) {
		return nil, nil
	}
	return nil, response.Error
}

func (h *hotLaneBinding) consumerAPISubject(operation string, name hotDurable) string {
	return "$JS." + JSDomain() + ".API.CONSUMER." + operation + "." + h.stream + "." + string(name)
}

func (h *hotLaneBinding) ensure() (jsapi.Consumer, error) {
	ctx, cancel := context.WithTimeout(h.ctx, pullProvisionTimeout)
	defer cancel()
	live, err := h.info(ctx, hotDurable(h.name))
	if err != nil {
		return nil, fmt.Errorf("bus: read hot lane %q: %w", h.name, err)
	}
	if live == nil {
		pullCreateStagger()
	}
	if live != nil {
		return h.ensureExisting(ctx, live)
	}
	return h.provision(ctx)
}

func (h *hotLaneBinding) ensureExisting(ctx context.Context, live *jsapi.ConsumerInfo) (jsapi.Consumer, error) {
	if err := validateLaneShape(live.Config, h.desired); err != nil {
		return nil, err
	}
	if live.Paused || pullConsumerConverged(live.Config, h.desired) {
		h.paused.Store(live.Paused)
		return h.pullHandle(ctx)
	}
	return h.provision(ctx)
}

func (h *hotLaneBinding) provision(ctx context.Context) (jsapi.Consumer, error) {
	err := h.withClaim(ctx, h.ensureDurable)
	if err != nil {
		return nil, fmt.Errorf("bus: provision hot lane %q: %w", h.name, err)
	}
	return h.pullHandle(ctx)
}

func (h *hotLaneBinding) pullHandle(ctx context.Context) (jsapi.Consumer, error) {
	if h.push != nil {
		return nil, nil
	}
	if err := retirePullPredecessor(ctx, h.js, h.stream, h.desired); err != nil {
		return nil, err
	}
	return h.js.Consumer(ctx, h.stream, h.name)
}

func (h *hotLaneBinding) withClaim(ctx context.Context, change func(context.Context) error) error {
	claim := h.claim
	claim.Description = nats.NewInbox()
	for {
		_, err := h.js.CreateConsumer(ctx, h.stream, claim)
		if err == nil {
			break
		}
		if !errors.Is(err, jsapi.ErrConsumerExists) {
			return fmt.Errorf("acquire handover claim: %w", err)
		}
		if !sleepOrDone(ctx, pullLeaderPoll) {
			return fmt.Errorf("wait for handover claim: %w", ctx.Err())
		}
	}
	defer h.releaseClaim()
	return change(ctx)
}

func (h *hotLaneBinding) releaseClaim() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	h.progressWarning(deletePullConsumer(ctx, h.js, h.stream, h.claim.Name))
}

func (h *hotLaneBinding) ensureDurable(ctx context.Context) error {
	live, err := h.info(ctx, hotDurable(h.name))
	if err != nil {
		return err
	}
	if live == nil {
		return h.ensureMissing(ctx)
	}
	if err := validateLaneShape(live.Config, h.desired); err != nil {
		return err
	}
	h.paused.Store(live.Paused)
	if err := h.updateDurable(ctx, live); err != nil {
		return err
	}
	return h.retireIngressStaging(ctx)
}

func (h *hotLaneBinding) ensureMissing(ctx context.Context) error {
	err := h.createFromPeer(ctx)
	if errors.Is(err, jsapi.ErrConsumerExists) {
		return h.ensureDurable(ctx)
	}
	return err
}

func (h *hotLaneBinding) updateDurable(ctx context.Context, live *jsapi.ConsumerInfo) error {
	if live.Paused || pullConsumerConverged(live.Config, h.desired) {
		return nil
	}
	desired := h.desired
	desired.DeliverPolicy = live.Config.DeliverPolicy
	desired.OptStartSeq = live.Config.OptStartSeq
	if h.push != nil {
		_, err := h.js.UpdatePushConsumer(ctx, h.stream, desired)
		return err
	}
	_, err := h.js.UpdateConsumer(ctx, h.stream, desired)
	return err
}

func (h *hotLaneBinding) createFromPeer(ctx context.Context) error {
	other, err := h.info(ctx, h.other)
	if err != nil {
		return err
	}
	if other != nil {
		return h.activateFromPeer(ctx, other)
	}
	desired, err := h.legacyPosition(ctx)
	if err != nil {
		return err
	}
	return h.activate(ctx, desired)
}

func (h *hotLaneBinding) legacyPosition(ctx context.Context) (jsapi.ConsumerConfig, error) {
	legacy := h.desired
	if h.push != nil {
		legacy.Name = string(h.other)
	}
	position, err := initialPullPosition(ctx, h.js, h.stream, legacy)
	desired := h.desired
	desired.DeliverPolicy, desired.OptStartSeq = position.DeliverPolicy, position.OptStartSeq
	return desired, err
}

func (h *hotLaneBinding) activateFromPeer(ctx context.Context, other *jsapi.ConsumerInfo) error {
	other, err := h.freezePeer(ctx, other)
	if err != nil {
		return err
	}
	if other == nil {
		return h.activate(ctx, h.desired)
	}
	desired, err := h.carryPeerFloor(ctx, other.AckFloor.Stream)
	if err != nil {
		return err
	}
	return h.activate(ctx, desired)
}

func (h *hotLaneBinding) freezePeer(ctx context.Context, other *jsapi.ConsumerInfo) (*jsapi.ConsumerInfo, error) {
	if other.Paused {
		return other, nil
	}
	return freezePredecessor(ctx, h.js, other)
}

func (h *hotLaneBinding) activate(ctx context.Context, desired jsapi.ConsumerConfig) error {
	if err := h.createDurable(ctx, desired); err != nil {
		return err
	}
	h.paused.Store(false)
	if err := h.retireIngressStaging(ctx); err != nil {
		return err
	}
	return deletePullConsumer(ctx, h.js, h.stream, h.name+"_reset_staging")
}

func (h *hotLaneBinding) carryPeerFloor(ctx context.Context, floor uint64) (jsapi.ConsumerConfig, error) {
	desired := h.desired
	stream, err := h.letters.js.StreamInfo(h.stream, nats.Context(ctx))
	if err != nil {
		return desired, err
	}
	if floor <= stream.State.LastSeq {
		desired.DeliverPolicy = jsapi.DeliverByStartSequencePolicy
		desired.OptStartSeq = floor + 1
	}
	return desired, nil
}

func (h *hotLaneBinding) createDurable(ctx context.Context, desired jsapi.ConsumerConfig) error {
	if h.push != nil {
		_, err := h.js.CreatePushConsumer(ctx, h.stream, desired)
		return err
	}
	_, err := h.js.CreateConsumer(ctx, h.stream, desired)
	return err
}

func (h *hotLaneBinding) retireIngressStaging(ctx context.Context) error {
	return deleteLaneStaging(ctx, h.js, h.stream, hotDurable(h.name))
}

func validateLaneShape(live, desired jsapi.ConsumerConfig) error {
	if consumerContract(live) > consumerContract(desired) {
		return fmt.Errorf("bus: consumer %q requires newer contract %d", desired.Name, consumerContract(live))
	}
	if !pullConsumerImmutableMatch(live, desired) {
		return fmt.Errorf("bus: consumer %q contract drift requires a newer versioned durable", desired.Name)
	}
	return nil
}
