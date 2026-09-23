// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watch

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/pkg/codec"
)

// The NATS servers as deploy/messaging declares them: the hub StatefulSet
// (app=nats) and the leaf DaemonSet (app=nats-leaf), both with a container
// named nats. The monitor port is read from the container's port named
// monitor instead of assumed to be 8222: the leaf listens on 8223, a split
// kept from when both shared the node netns, and only the Services publish
// 8222 for it.
const (
	natsNamespace   ports.Namespace = "messaging"
	natsSelector                    = "app in (nats,nats-leaf)"
	natsContainer                   = "nats"
	monitorPortName                 = "monitor"
)

// varz is the part of /varz this package reads. config_load_time is the
// field name in nats-server 2.14 (server/monitor.go Varz.ConfigLoadTime),
// set at start and on every successful reload.
type varz struct {
	ConfigLoadTime time.Time `json:"config_load_time"`
}

// NATSServers errors on the first server it cannot read, pod without an IP
// included: the acl stage needs every server's answer, and a partial list
// would read as "all reloaded".
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
	url := "http://" + net.JoinHostPort(p.Status.PodIP, strconv.Itoa(int(port))) + "/varz"
	resp, err := w.get(ctx, url)
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
