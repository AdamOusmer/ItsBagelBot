// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/pkg/bus"
	"context"
	"errors"
	"fmt"
	"time"

	jsapi "github.com/nats-io/nats.go/jetstream"
)

func streamLeader(ctx context.Context, js jsapi.JetStream, stream string) (string, error) {
	st, err := js.Stream(ctx, stream)
	if err != nil {
		return "", err
	}
	info, err := st.Info(ctx)
	if err != nil || info.Cluster == nil {
		return "", errors.Join(err, errors.New("bus-bench: stream has no cluster info"))
	}
	return info.Cluster.Leader, nil
}

func consumerLeader(ctx context.Context, js jsapi.JetStream, stream, durable string) (string, error) {
	c, err := js.Consumer(ctx, stream, durable)
	if err != nil {
		return "", err
	}
	info, err := c.Info(ctx)
	if err != nil || info.Cluster == nil {
		return "", errors.Join(err, errors.New("bus-bench: consumer has no cluster info"))
	}
	return info.Cluster.Leader, nil
}

type seatReport struct {
	Consumer string `json:"consumer"`
	Leader   string `json:"leader"`
	Stream   string `json:"stream_leader"`
	Attempts int    `json:"attempts"`
}

func runSeatConsumer(lane benchLane, preferred string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	nc, js, err := mgmtConnect(lane.url)
	if err != nil {
		return err
	}
	defer nc.Close()

	report := seatReport{Consumer: durableFor(lane)}
	if report.Stream, err = streamLeader(ctx, js, lane.stream); err != nil {
		return err
	}
	for report.Attempts < 12 && ctx.Err() == nil {
		report.Attempts++
		report.Leader, err = recreateBenchConsumer(ctx, js, lane)
		if err == nil && seated(preferred, report.Stream, report.Leader) {
			emit(report)
			return nil
		}
	}
	emit(report)
	return errors.Join(err, fmt.Errorf("bus-bench: could not seat consumer leader %s", preferred))
}

func seated(preferred, streamLeader, leader string) bool {
	if preferred == "stream-leader" {
		return leader == streamLeader
	}
	return leader != "" && leader != streamLeader
}

func recreateBenchConsumer(ctx context.Context, js jsapi.JetStream, lane benchLane) (string, error) {
	if _, err := deleteBenchConsumer(ctx, js, lane.stream, durableFor(lane)); err != nil {
		return "", err
	}
	if _, err := js.CreateConsumer(ctx, lane.stream, bus.PullLaneConsumerConfig(lane.group, lane.subject)); err != nil {
		return "", err
	}
	deadline := time.Now().Add(5 * time.Second)
	leader := ""
	for leader == "" && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		leader, _ = consumerLeader(ctx, js, lane.stream, durableFor(lane))
	}
	return leader, nil
}
