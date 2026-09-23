// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watch

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"ItsBagelBot/app/deployer/internal/ports"
)

// varzServer answers /varz the way nats-server 2.14 does, trimmed to a few
// neighbouring fields so the decoder is shown to pick config_load_time out
// of a larger document.
func varzServer(t *testing.T, status int, loaded time.Time) (int32, func()) {
	t.Helper()
	body := `{"server_id":"NTEST","version":"2.14.6","start":"2026-09-20T08:00:00Z","config_load_time":"` +
		loaded.Format(time.RFC3339Nano) + `","connections":4}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/varz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	return int32(srv.Listener.Addr().(*net.TCPAddr).Port), srv.Close
}

// natsSpec is a messaging pod whose nats container declares its monitor
// port under the name the manifests use. port 0 declares none.
type natsSpec struct {
	name, app, node, ip string
	port                int32
}

func (s natsSpec) pod() podSpec {
	c := corev1.Container{Name: natsContainer}
	if s.port != 0 {
		c.Ports = []corev1.ContainerPort{{Name: monitorPortName, ContainerPort: s.port}}
	}
	return podSpec{name: s.name, ns: natsNamespace, labels: map[string]string{"app": s.app}, node: s.node, ip: s.ip, containers: []corev1.Container{c}}
}

func TestNATSServers(t *testing.T) {
	hubLoaded := testNow.Add(-90 * time.Second)
	leafLoaded := testNow.Add(-5 * time.Second)
	hubPort, closeHub := varzServer(t, http.StatusOK, hubLoaded)
	defer closeHub()
	leafPort, closeLeaf := varzServer(t, http.StatusOK, leafLoaded)
	defer closeLeaf()
	brokenPort, closeBroken := varzServer(t, http.StatusServiceUnavailable, testNow)
	defer closeBroken()

	hub := natsSpec{name: "nats-0", app: "nats", node: "node1", ip: "127.0.0.1", port: hubPort}.pod()
	leaf := natsSpec{name: "nats-leaf-x7k2p", app: "nats-leaf", node: "node2", ip: "127.0.0.1", port: leafPort}.pod()
	leaving := natsSpec{name: "nats-1", app: "nats", node: "node3", port: hubPort}.pod()
	leaving.deleting = true
	// The exporter's own app label keeps it out: it serves no /varz of its own.
	exporter := natsSpec{name: "surveyor-0", app: "nats-surveyor", node: "node1", ip: "127.0.0.1", port: brokenPort}.pod()

	cases := []struct {
		name    string
		pods    []podSpec
		want    []ports.NATSServer
		wantErr bool
	}{
		{
			name: "hub and leaf, terminating and foreign pods skipped",
			pods: []podSpec{hub, leaf, leaving, exporter},
			want: []ports.NATSServer{
				{Pod: "nats-0", Node: "node1", ConfigLoaded: hubLoaded},
				{Pod: "nats-leaf-x7k2p", Node: "node2", ConfigLoaded: leafLoaded},
			},
		},
		{name: "server without an ip yet", pods: []podSpec{hub, natsSpec{name: "nats-2", app: "nats", port: hubPort}.pod()}, wantErr: true},
		{name: "server without a monitor port", pods: []podSpec{natsSpec{name: "nats-2", app: "nats", node: "node3", ip: "127.0.0.1"}.pod()}, wantErr: true},
		{name: "varz not answering 200", pods: []podSpec{natsSpec{name: "nats-2", app: "nats", node: "node3", ip: "127.0.0.1", port: brokenPort}.pod()}, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objs := make([]runtime.Object, 0, len(c.pods))
			for _, p := range c.pods {
				objs = append(objs, p.obj())
			}
			got, err := testWatcher(objs...).NATSServers(context.Background())
			if (err != nil) != c.wantErr {
				t.Fatalf("NATSServers() error = %v, want error %v", err, c.wantErr)
			}
			if !reflect.DeepEqual(got, c.want) && !c.wantErr {
				t.Fatalf("NATSServers() = %+v, want %+v", got, c.want)
			}
		})
	}
}
