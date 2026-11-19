// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"strings"

	jsapi "github.com/nats-io/nats.go/jetstream"
)

type ingressProvisioner interface {
	streamProvisioner
	pullConsumerProvisioner
}

func hotStream(spec StreamSpec) bool {
	return spec.Name == TwitchIngressPremiumStream.Name || spec.Name == TwitchIngressStandardStream.Name
}

func (g *streamGuardian) prepareIngress(ctx context.Context) error {
	for _, spec := range g.specs {
		if err := g.prepareHotStream(ctx, spec); err != nil {
			return err
		}
	}
	return nil
}

func (g *streamGuardian) prepareHotStream(ctx context.Context, spec StreamSpec) error {
	if !hotStream(spec) {
		return nil
	}
	js, ok := g.js.(ingressProvisioner)
	if !ok {
		return fmt.Errorf("bus: ingress provisioning requires consumer management")
	}
	if err := prepareStagingStream(ctx, js, spec); err != nil {
		return err
	}
	desired := stagedConsumer(spec, g.group)
	name := hotDurable(strings.TrimSuffix(desired.Name, "_staging"))
	active, err := pullConsumerInfo(ctx, js, spec.Name, string(name))
	if err != nil {
		return err
	}
	if active != nil {
		return deleteLaneStaging(ctx, js, spec.Name, name)
	}
	_, err = js.CreateConsumer(ctx, spec.Name, desired)
	if err != nil && !errors.Is(err, jsapi.ErrConsumerExists) {
		return err
	}
	return retireStagingIfActive(ctx, js, spec.Name, name)
}

func prepareStagingStream(ctx context.Context, js streamProvisioner, spec StreamSpec) error {
	stage := spec
	stage.Subjects = []string{"bus.ingress.staging." + spec.Name}
	stage.MaxBytes = 1 << 20
	_, err := js.Stream(ctx, spec.Name)
	if errors.Is(err, jsapi.ErrStreamNotFound) {
		_, err = js.CreateStream(ctx, streamConfig(stage))
	}
	if errors.Is(err, jsapi.ErrStreamNameAlreadyInUse) {
		return nil
	}
	return err
}

func stagedConsumer(spec StreamSpec, group string) jsapi.ConsumerConfig {
	desired := pullConsumerConfig(spec.Subjects[0], HotLaneConsumerName(group, spec.Subjects[0])+"_staging")
	desired.Replicas = spec.Replicas
	desired.FilterSubject = ""
	return desired
}

func retireStagingIfActive(ctx context.Context, js pullConsumerProvisioner, stream string, name hotDurable) error {
	active, err := pullConsumerInfo(ctx, js, stream, string(name))
	if err != nil || active == nil {
		return err
	}
	return deleteLaneStaging(ctx, js, stream, name)
}

func deleteLaneStaging(ctx context.Context, js pullConsumerProvisioner, stream string, name hotDurable) error {
	for _, mode := range []hotDurable{name, otherModeDurable(name)} {
		if err := deletePullConsumer(ctx, js, stream, string(mode)+"_staging"); err != nil {
			return err
		}
	}
	return nil
}

func (g *streamGuardian) splitsPremium(spec StreamSpec) bool {
	if spec.Name != TwitchIngressStream.Name {
		return false
	}
	for _, owned := range g.specs {
		if owned.Name == TwitchIngressPremiumStream.Name {
			return true
		}
	}
	return false
}

func (g *streamGuardian) detachPremiumPredecessor(ctx context.Context, spec StreamSpec) error {
	if !g.splitsPremium(spec) {
		return nil
	}
	js := g.js.(ingressProvisioner)
	stream, err := js.Stream(ctx, spec.Name)
	if errors.Is(err, jsapi.ErrStreamNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	legacy := stream.CachedInfo().Config
	legacy.Subjects = spec.Subjects
	if _, err := js.UpdateStream(ctx, legacy); err != nil {
		return err
	}
	desired := PullLaneConsumerConfig(g.group, TwitchIngressPremiumStream.Subjects[0])
	_, err = pausePredecessor(ctx, js, TwitchIngressPremiumStream.Name, desired)
	return err
}
