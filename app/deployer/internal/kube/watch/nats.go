// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watch

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/env"
)

const (
	natsNamespace   ports.Namespace = "messaging"
	natsSelector                    = "app in (nats,nats-leaf)"
	natsContainer                   = "nats"
	monitorPortName                 = "monitor"
)

type varz struct {
	ConfigLoadTime time.Time `json:"config_load_time"`
}

// Errors on the first unreadable server: a partial list would read as all reloaded.
func (w *Watcher) NATSServers(ctx context.Context) ([]ports.NATSServer, error) {
	pods, err := w.listPods(ctx, natsNamespace, natsSelector)
	if err != nil {
		return nil, err
	}
	pods = slices.DeleteFunc(pods, func(p corev1.Pod) bool { return p.DeletionTimestamp != nil })
	out := make([]ports.NATSServer, 0, len(pods))
	for i := range pods {
		loaded, err := w.configLoaded(ctx, &pods[i])
		if err != nil {
			return nil, err
		}
		out = append(out, ports.NATSServer{Pod: pods[i].Name, Node: pods[i].Spec.NodeName, ConfigLoaded: loaded})
	}
	return out, nil
}

func (w *Watcher) configLoaded(ctx context.Context, p *corev1.Pod) (time.Time, error) {
	port, ok := monitorPort(p)
	if !ok || p.Status.PodIP == "" {
		return time.Time{}, fmt.Errorf("nats pod %s: no monitor address yet", p.Name)
	}
	url := "https://" + net.JoinHostPort(p.Status.PodIP, strconv.Itoa(int(port))) + "/varz"
	resp, err := w.getWith(ctx, w.natsClient(p.Labels["app"]), url)
	if err != nil {
		return time.Time{}, fmt.Errorf("nats pod %s: %w", p.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("nats pod %s: /varz answered %d", p.Name, resp.StatusCode)
	}
	var v varz
	if err := codec.NewDecoder(resp.Body).Decode(&v); err != nil {
		return time.Time{}, fmt.Errorf("nats pod %s: decode /varz: %w", p.Name, err)
	}
	return v.ConfigLoadTime, nil
}

// Pods are dialed by IP, so the certificate is checked against the Service name of their app.
func (w *Watcher) natsClient(app string) *http.Client {
	client := *w.http
	client.Transport = &http.Transport{
		DisableKeepAlives: true,
		TLSClientConfig: &tls.Config{
			RootCAs:    w.natsRoots,
			ServerName: w.natsServerName(app),
			MinVersion: tls.VersionTLS12,
		},
	}
	return &client
}

func natsServiceName(app string) string {
	return app + "." + string(natsNamespace) + ".svc.cluster.local"
}

func natsRootsFromEnv() *x509.CertPool {
	pem := env.Get("NATS_CA_PEM", "")
	if pem == "" {
		return nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(pem)) {
		return nil
	}
	return pool
}

func monitorPort(p *corev1.Pod) (int32, bool) {
	i := slices.IndexFunc(p.Spec.Containers, func(c corev1.Container) bool { return c.Name == natsContainer })
	if i < 0 {
		return 0, false
	}
	cps := p.Spec.Containers[i].Ports
	j := slices.IndexFunc(cps, func(cp corev1.ContainerPort) bool { return cp.Name == monitorPortName })
	if j < 0 {
		return 0, false
	}
	return cps[j].ContainerPort, true
}
