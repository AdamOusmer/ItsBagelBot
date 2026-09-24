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
	return confirmed(servers, *at), nil
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
	changed, err := a.changed(ctx, run)
	if err != nil {
		return time.Time{}, err
	}
	if !changed {
		return time.Time{}, stage.ErrSkipped
	}
	return a.apply(ctx, pinRef(run))
}

func (a aclRun) changed(ctx context.Context, run deploy.Run) (bool, error) {
	changed, err := a.diff(ctx, run)
	if err != nil {
		return false, err
	}
	return changed, a.rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.MessagingChanged = changed })
}

func (a aclRun) diff(ctx context.Context, run deploy.Run) (bool, error) {
	live := run.Outputs.LiveSHA
	if live == "" {
		return true, nil
	}
	cmp, err := a.rc.Deps.GitHub.Compare(ctx, ports.Ref(live), pinRef(run))
	if err != nil {
		return false, ports.Failf(deploy.FailGitHub, "compare %s...%s: %v", short(live), pinRef(run), err)
	}
	return touches(cmp.Files, a.rc.Deps.Config.MessagingDir), nil
}

func touches(files []ports.FilePath, dir ports.FilePath) bool {
	for _, f := range files {
		if strings.HasPrefix(string(f), string(dir)+"/") {
			return true
		}
	}
	return false
}

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
	if !reloads(res.Changed) {
		return time.Time{}, nil
	}
	return start, a.rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.ACLAppliedAt = &start })
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
	return confirmed(servers, w.at), nil
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

func confirmed(servers []ports.NATSServer, at time.Time) bool {
	return len(servers) > 0 && reloaded(servers, at) == len(servers)
}
