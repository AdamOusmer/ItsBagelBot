// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var embedded struct {
	once    sync.Once
	dir     string
	servers []*server.Server
	url     string
	err     error
}

func TestMain(m *testing.M) {
	code := m.Run()
	for _, s := range embedded.servers {
		s.Shutdown()
		s.WaitForShutdown()
	}
	if embedded.dir != "" {
		_ = os.RemoveAll(embedded.dir)
	}
	os.Exit(code)
}

func brokerURL(tb testing.TB) string {
	tb.Helper()
	if url := os.Getenv("NATS_INTEGRATION_URL"); url != "" {
		return url
	}
	embedded.once.Do(startEmbeddedCluster)
	if embedded.err != nil {
		tb.Fatalf("embedded NATS cluster: %v", embedded.err)
	}
	return embedded.url
}

func startEmbeddedCluster() { embedded.err = launchCluster() }

func launchCluster() error {
	dir, err := os.MkdirTemp("", "bus-cluster-")
	if err != nil {
		return err
	}
	embedded.dir = dir
	ports, err := freePorts(3)
	if err != nil {
		return err
	}
	if err := startNodes(ports); err != nil {
		return err
	}
	for _, node := range embedded.servers {
		if !node.ReadyForConnections(30 * time.Second) {
			return fmt.Errorf("node %s not ready", node.Name())
		}
	}
	embedded.url = embedded.servers[0].ClientURL()
	return awaitMetaLeader()
}

func startNodes(ports []int) error {
	var routes []*url.URL
	for _, port := range ports {
		routes = append(routes, &url.URL{Scheme: "nats-route", Host: fmt.Sprintf("127.0.0.1:%d", port)})
	}
	for _, port := range ports {
		node, err := startClusterNode(port, routes)
		if err != nil {
			return err
		}
		embedded.servers = append(embedded.servers, node)
	}
	return nil
}

func freePorts(n int) ([]int, error) {
	listeners := make([]net.Listener, 0, n)
	ports := make([]int, 0, n)
	for range n {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		listeners = append(listeners, ln)
		ports = append(ports, ln.Addr().(*net.TCPAddr).Port)
	}
	for _, ln := range listeners {
		_ = ln.Close()
	}
	return ports, nil
}

func startClusterNode(clusterPort int, routes []*url.URL) (*server.Server, error) {
	node, err := server.NewServer(&server.Options{
		ServerName: fmt.Sprintf("n%d", clusterPort), Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
		JetStream: true, JetStreamDomain: "hub", StoreDir: filepath.Join(embedded.dir, fmt.Sprint(clusterPort)),
		JetStreamMaxMemory: 2 << 30, JetStreamMaxStore: 1 << 30,
		Cluster: server.ClusterOpts{Name: "bagel", Host: "127.0.0.1", Port: clusterPort},
		Routes:  routes,
	})
	if err != nil {
		return nil, err
	}
	node.Start()
	return node, nil
}

func awaitMetaLeader() error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := probeJetStream()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func probeJetStream() error {
	nc, err := nats.Connect(embedded.url)
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := jsapi.NewWithDomain(nc, "hub")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = js.AccountInfo(ctx)
	return err
}

var catalog struct {
	once sync.Once
	err  error
}

func catalogSpecs() []StreamSpec {
	return append(fleetStreamSpecs(), BagelDeadLetterStream, DiscordIngressStream, DiscordOutgressStream)
}

type busFixture struct {
	url    string
	nc     *nats.Conn
	js     nats.JetStreamContext
	modern jsapi.JetStream
}

func newBusFixture(t *testing.T) busFixture {
	t.Helper()
	t.Setenv("NATS_INGRESS_PARTITION", "on")
	url := brokerURL(t)
	catalog.once.Do(func() {
		catalog.err = EnsureStreams(context.Background(), url, catalogSpecs(), zap.NewNop())
	})
	require.NoError(t, catalog.err)
	nc, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := nc.JetStream(nats.Domain(JSDomain()))
	require.NoError(t, err)
	modern, err := jsapi.NewWithDomain(nc, JSDomain())
	require.NoError(t, err)
	fixture := busFixture{url: url, nc: nc, js: js, modern: modern}
	for _, spec := range catalogSpecs() {
		require.NoError(t, js.PurgeStream(spec.Name))
	}
	return fixture
}

func (f busFixture) storedMessages(t *testing.T, stream string) uint64 {
	t.Helper()
	info, err := f.js.StreamInfo(stream)
	require.NoError(t, err)
	return info.State.Msgs
}

func (f busFixture) messageCount(spec StreamSpec) uint64 {
	info, err := f.js.StreamInfo(spec.Name)
	if err != nil {
		return 0
	}
	return info.State.Msgs
}

func (f busFixture) appendMessage(t *testing.T, subject string, payload string, headers ...string) {
	t.Helper()
	msg := nats.NewMsg(subject)
	msg.Data = []byte(payload)
	for i := 0; i+1 < len(headers); i += 2 {
		msg.Header.Add(headers[i], headers[i+1])
	}
	_, err := f.js.PublishMsg(msg)
	require.NoError(t, err)
}

func privateBroker(t *testing.T) string {
	t.Helper()
	s, err := server.NewServer(&server.Options{
		Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
		JetStream: true, JetStreamDomain: "hub", StoreDir: t.TempDir(),
	})
	require.NoError(t, err)
	s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second))
	t.Cleanup(func() {
		s.Shutdown()
		s.WaitForShutdown()
	})
	return s.ClientURL()
}

func jetStreamOf(t *testing.T, url string) nats.JetStreamContext {
	t.Helper()
	nc, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := nc.JetStream(nats.Domain(JSDomain()))
	require.NoError(t, err)
	return js
}

func coreServer(t *testing.T, name string) *server.Server {
	t.Helper()
	s, err := server.NewServer(&server.Options{ServerName: name, Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second))
	t.Cleanup(func() {
		s.Shutdown()
		s.WaitForShutdown()
	})
	return s
}
