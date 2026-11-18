// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type pushTestCluster struct {
	configs map[string]clusterNodeConfig
	nodes   map[string]*server.Server
	url     string
}

func privateCluster(t *testing.T) *pushTestCluster {
	t.Helper()
	ports, err := freePorts(6)
	require.NoError(t, err)
	routes, store := clusterRoutes(ports[:3]), t.TempDir()
	c := &pushTestCluster{configs: make(map[string]clusterNodeConfig), nodes: make(map[string]*server.Server)}
	var urls []string
	for i, port := range ports[:3] {
		cfg := clusterNodeConfig{routePort: port, clientPort: ports[i+3], routes: routes, store: store}
		name := fmt.Sprintf("n%d", port)
		c.configs[name] = cfg
		c.start(t, name)
		urls = append(urls, c.nodes[name].ClientURL())
	}
	c.url = strings.Join(urls, ",")
	return c
}

func (c *pushTestCluster) start(t *testing.T, name string) {
	t.Helper()
	node, err := startClusterNode(c.configs[name])
	require.NoError(t, err)
	t.Cleanup(node.Shutdown)
	require.True(t, node.ReadyForConnections(5*time.Second))
	c.nodes[name] = node
}

func (c *pushTestCluster) stop(name string) {
	node := c.nodes[name]
	node.Shutdown()
	node.WaitForShutdown()
}

func (c *pushTestCluster) fullRestart(t *testing.T) {
	t.Helper()
	for name := range c.configs {
		c.stop(name)
	}
	for name := range c.configs {
		c.start(t, name)
	}
}

func (c *pushTestCluster) fixture(t *testing.T, spec StreamSpec) pushFixture {
	t.Helper()
	nc, err := nats.Connect(c.url, nats.MaxReconnects(-1), nats.ReconnectWait(50*time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := nc.JetStream(nats.Domain(JSDomain()))
	require.NoError(t, err)
	modern, err := jsapi.NewWithDomain(nc, JSDomain())
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()
		_, err := modern.AccountInfo(ctx)
		return err == nil
	}, 12*time.Second, 100*time.Millisecond)
	require.NoError(t, EnsureStreams(t.Context(), c.url, []StreamSpec{spec}, zap.NewNop()))
	return pushFixture{busFixture: busFixture{url: c.url, nc: nc, js: js, modern: modern}, spec: spec}
}

func TestPushLaneFullClusterRestart(t *testing.T) {
	hotLaneTestEnv(t)
	cluster := privateCluster(t)
	f := cluster.fixture(t, TwitchIngressPremiumStream)
	f.positionAtThousand(t)
	cluster.fullRestart(t)
	stream := f.readyStream(t)
	info := f.consumer(t, f.pushName())
	t.Logf("after restart: stream last=%d, consumer delivered=%d, configured start=%d",
		stream.State.LastSeq, info.Delivered.Stream, info.Config.OptStartSeq)
	require.Equal(t, [3]uint64{0, 999, 1000}, [3]uint64{stream.State.LastSeq, info.Delivered.Stream, info.Config.OptStartSeq})

	sub, err := NewSubscriber(f.url, pushTestGroup, zaptest.NewLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })
	inbox := subscribe(t, sub, f.subject())
	f.publishIDs(t, 1000, 1010)
	ackIDs(t, inbox, 1000, 1010)
	f.awaitEmpty(t)
	assert.Equal(t, jsapi.DeliverAllPolicy, f.consumer(t, f.pushName()).Config.DeliverPolicy)
}

func (f pushFixture) positionAtThousand(t *testing.T) {
	t.Helper()
	t.Setenv("NATS_HOT_LANE_MODE", "pull")
	pull, inbox := f.open(t)
	f.publishIDs(t, 1, 1000)
	ackIDs(t, inbox, 1, 1000)
	assert.Equal(t, uint64(999), f.awaitAcked(t, f.pullName()))
	t.Setenv("NATS_HOT_LANE_MODE", "push")
	push, _ := f.open(t)
	require.Equal(t, uint64(1000), f.consumer(t, f.pushName()).Config.OptStartSeq)
	require.NoError(t, pull.Close())
	require.NoError(t, push.Close())
}

func TestPushLaneLeaderServerShutdownKeepsEveryPublishedID(t *testing.T) {
	hotLaneTestEnv(t)
	t.Setenv("NATS_PULL_ACK_WAIT", "4s")
	cluster := privateCluster(t)
	f := cluster.fixture(t, TwitchIngressStandardStream)
	sub, inbox := f.open(t)
	var seen sync.Map
	go collectFailoverIDs(inbox, &seen)

	stop, published := f.publishDuringFailover(t)
	consumer := consumerLeaderName(f.consumer(t, f.pushName()))
	require.NotEmpty(t, consumer)
	cluster.cycle(t, f, consumer)
	cluster.cycle(t, f, f.readyStream(t).Cluster.Leader)
	close(stop)
	count := <-published
	require.Positive(t, count)
	require.Eventually(t, func() bool { return publishedIDsSeen(&seen, count) }, 16*time.Second, 50*time.Millisecond)
	f.awaitEmpty(t)
	assert.True(t, SubscriberHealthy(sub))
}

func (c *pushTestCluster) cycle(t *testing.T, f pushFixture, name string) {
	t.Helper()
	c.stop(name)
	require.Eventually(t, func() bool {
		info, err := f.lookup(t, f.pushName())
		return err == nil && consumerLeaderName(info) != "" && consumerLeaderName(info) != name
	}, 10*time.Second, 50*time.Millisecond)
	c.start(t, name)
	f.readyStream(t)
}

func collectFailoverIDs(inbox <-chan *Message, seen *sync.Map) {
	for msg := range inbox {
		seen.Store(string(msg.Payload), true)
		msg.Ack()
	}
}

func publishedIDsSeen(seen *sync.Map, count int) bool {
	for id := range count {
		if _, ok := seen.Load(strconv.Itoa(id)); !ok {
			return false
		}
	}
	return true
}

func (f pushFixture) publishDuringFailover(t *testing.T) (chan struct{}, <-chan int) {
	t.Helper()
	stop, done := make(chan struct{}), make(chan int, 1)
	prefix := nats.NewInbox() + "."
	go func() {
		for id := 0; ; id++ {
			if !f.publishWithRetry(t.Context(), prefix, id) {
				done <- -1
				return
			}
			select {
			case <-stop:
				done <- id + 1
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	return stop, done
}

func (f pushFixture) publishWithRetry(ctx context.Context, prefix string, id int) bool {
	msg := nats.NewMsg(f.subject())
	msg.Data = []byte(strconv.Itoa(id))
	msg.Header.Set(nats.MsgIdHdr, prefix+string(msg.Data))
	for {
		_, err := f.js.PublishMsg(msg, nats.AckWait(500*time.Millisecond))
		if err == nil {
			return true
		}
		if !sleepOrDone(ctx, 50*time.Millisecond) {
			return false
		}
	}
}
