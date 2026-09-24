// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

type acl struct{}

func (acl) ID() deploy.StageID { return deploy.StageACL }

func (acl) Done(ctx context.Context, rc *stage.RunCtx) (bool, error) {
	at := rc.View().Outputs.ACLAppliedAt
	if at == nil {
		return false, nil
	}
	servers, err := rc.Deps.Watcher.NATSServers(ctx)
	if err != nil {
		return false, nil
	}
	return quiet(servers, *at, rc.Deps.Clock.Now(), rc.Deps.Config.ACLSettle), nil
}

func (acl) Run(ctx context.Context, rc *stage.RunCtx) error {
	a := aclRun{rc: rc}
	at, err := a.appliedAt(ctx)
	if err != nil {
		return err
	}
	if at.IsZero() {
		rc.SetItems(ctx, []deploy.Item{{Key: "reload", Label: "NATS config reload", State: deploy.StateSkipped,
			Detail: "the apply changed nothing the NATS servers load"}})
		return nil
	}
	return a.confirm(ctx, at)
}

type aclRun struct{ rc *stage.RunCtx }

func (a aclRun) appliedAt(ctx context.Context) (time.Time, error) {
	run := a.rc.View()
	if at := run.Outputs.ACLAppliedAt; at != nil {
		return *at, nil
	}
	return a.apply(ctx, pinRef(run))
}

// Always apply: the live config can drift from git without any commit touching the messaging dir.
// Take the time before Apply: a reload that lands before Apply returns would never confirm.
func (a aclRun) apply(ctx context.Context, ref ports.Ref) (time.Time, error) {
	dir := a.rc.Deps.Config.MessagingDir
	objs, err := build(ctx, a.rc, dir, ref)
	if err != nil {
		return time.Time{}, err
	}
	start := a.rc.Deps.Clock.Now()
	res, err := a.rc.Deps.Applier.Apply(ctx, managedOnly(objs))
	if err != nil {
		return time.Time{}, applyErr(string(dir), err)
	}
	changed := len(res.Changed) > 0
	if !reloads(res.Changed) {
		return time.Time{}, a.rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.MessagingChanged = changed })
	}
	return start, a.rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.MessagingChanged, o.ACLAppliedAt = changed, &start })
}

func reloads(changed []ports.ObjectRef) bool {
	for _, c := range changed {
		switch c.Kind {
		case kindConfigMap, kindDeployment, kindDaemonSet:
			return true
		}
	}
	return false
}

func (a aclRun) confirm(ctx context.Context, at time.Time) error {
	limit := a.rc.Deps.Config.ACLTimeout
	wait, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	w := &reloadWatch{rc: a.rc, at: at}
	err := poll(wait, a.rc, w.check)
	if timedOut(ctx, err) {
		return w.timeout(limit)
	}
	return err
}

type reloadWatch struct {
	rc      *stage.RunCtx
	at      time.Time
	servers []ports.NATSServer
	lastErr error
	shown   string
}

func (w *reloadWatch) check(ctx context.Context) (bool, error) {
	servers, err := w.rc.Deps.Watcher.NATSServers(ctx)
	if err != nil {
		w.lastErr = err
		return false, nil
	}
	w.servers = servers
	w.report(ctx)
	return quiet(servers, w.at, w.rc.Deps.Clock.Now(), w.rc.Deps.Config.ACLSettle), nil
}

func (w *reloadWatch) report(ctx context.Context) {
	items := make([]deploy.Item, len(w.servers))
	var sig strings.Builder
	for i, s := range w.servers {
		items[i] = serverItem(s, w.at)
		fmt.Fprintf(&sig, "%s=%s;", s.Pod, items[i].State)
	}
	if sig.String() == w.shown {
		return
	}
	w.shown = sig.String()
	w.rc.SetItems(ctx, items)
	w.rc.SetProgress(ctx, deploy.Progress{Done: reloaded(w.servers, w.at), Total: len(w.servers)})
}

func serverItem(s ports.NATSServer, at time.Time) deploy.Item {
	it := deploy.Item{Key: s.Pod, Label: s.Pod + " (" + s.Node + ")", State: deploy.StateRunning, Progress: deploy.Progress{Total: 1}}
	if s.ConfigLoaded.After(at) {
		it.State, it.Progress.Done = deploy.StateSucceeded, 1
	}
	return it
}

func (w *reloadWatch) timeout(limit time.Duration) error {
	f := ports.Failf(deploy.FailTimeout, "%d of %d NATS servers reloaded the config applied at %s within %s",
		reloaded(w.servers, w.at), len(w.servers), w.at.Format(time.RFC3339), limit)
	if allReloaded(w.servers, w.at) {
		f = ports.Failf(deploy.FailTimeout, "NATS servers reloaded but did not stay quiet for %s within %s",
			w.rc.Deps.Config.ACLSettle, limit)
	}
	for _, s := range w.servers {
		if !s.ConfigLoaded.After(w.at) {
			f.LogTail = append(f.LogTail, fmt.Sprintf("%s on %s: config loaded %s", s.Pod, s.Node, s.ConfigLoaded.Format(time.RFC3339)))
		}
	}
	if w.lastErr != nil {
		f.LogTail = append(f.LogTail, "last monitoring error: "+w.lastErr.Error())
	}
	return f
}

func reloaded(servers []ports.NATSServer, at time.Time) int {
	n := 0
	for _, s := range servers {
		if s.ConfigLoaded.After(at) {
			n++
		}
	}
	return n
}

func allReloaded(servers []ports.NATSServer, at time.Time) bool {
	return len(servers) > 0 && reloaded(servers, at) == len(servers)
}

// A reload in progress refuses hub-domain JetStream calls, and a ConfigMap swap sends one SIGHUP per file.
func quiet(servers []ports.NATSServer, at, now time.Time, settle time.Duration) bool {
	return allReloaded(servers, at) && now.Sub(lastLoad(servers)) >= settle
}

func lastLoad(servers []ports.NATSServer) time.Time {
	var last time.Time
	for _, s := range servers {
		if s.ConfigLoaded.After(last) {
			last = s.ConfigLoaded
		}
	}
	return last
}
