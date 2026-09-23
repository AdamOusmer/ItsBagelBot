// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cluster

import (
	"context"
	"fmt"
	"maps"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// rollout applies the pin commit's manifests and rolls the services one at
// a time in Order, each only once the one before it is fully available.
type rollout struct{}

func (rollout) ID() deploy.StageID { return deploy.StageRollout }

// Done reports that an earlier attempt ran every step (its last row, the
// status routes, succeeded) and that the pinned images are in every rolled
// service's pod template with every one of them settled, so a run whose
// stage finished but was never recorded moves on instead of rolling again.
// Images alone were not enough: a status-routes apply that failed after
// every service rolled left the images matching, and the resumed stage
// passed without ever applying deploy/db/status-routes.yaml. A resume from
// anywhere earlier runs again, which costs a no-op apply and an instant wait
// per settled service. A reapply is never done this way: it exists to push
// manifest changes that leave the images alone, so matching images say
// nothing about whether its work landed.
func (rollout) Done(ctx context.Context, rc *stage.RunCtx) (bool, error) {
	run := rc.View()
	if run.Kind == deploy.KindReapply || !stepsFinished(&run) {
		return false, nil
	}
	t, err := loadTarget(ctx, rc)
	if err != nil {
		return false, err
	}
	return rolledOut(ctx, rc.Deps.Watcher, managedUnits(t.layout.units))
}

// stepsFinished reports the stage's last row succeeded or skipped: the
// previous attempt reached the end of its steps.
func stepsFinished(run *deploy.Run) bool {
	s := run.Stage(deploy.StageRollout)
	if s == nil || len(s.Items) == 0 {
		return false
	}
	last := s.Items[len(s.Items)-1].State
	return last == deploy.StateSucceeded || last == deploy.StateSkipped
}

func (rollout) Run(ctx context.Context, rc *stage.RunCtx) error {
	t, err := loadTarget(ctx, rc)
	if err != nil {
		return err
	}
	r := rollRun{rc: rc}
	if rc.Cancelled() {
		return r.finishInFlight(ctx, t)
	}
	return runSteps(ctx, rc, r.steps(t))
}

// finishInFlight is the rollout a restarted deployer runs for a cancel that
// landed mid-stage: the engine hands the stage back so it can keep its
// promise to finish the service in flight. That service is found in the
// cluster, not in the stored rows (row writes are coalesced and can lag the
// apply): the first managed unit whose pinned images are applied but not
// yet settled. It is waited on, never applied again, and nothing after it
// starts.
func (r rollRun) finishInFlight(ctx context.Context, t target) error {
	u, ok, err := inFlightUnit(ctx, r.rc.Deps.Watcher, managedUnits(t.layout.units))
	if err != nil || !ok {
		return orCancelled(err)
	}
	s := r.service(u)
	r.rc.SetItem(ctx, s.row(deploy.StateRunning, "cancel requested: finishing the service in flight"))
	err = r.wait(ctx, u, s.last)
	r.rc.SetItem(ctx, s.result(err))
	return orCancelled(err)
}

// orCancelled is err, or stage.ErrCancelled once the step it ends went well:
// the cancel is honoured either way.
func orCancelled(err error) error {
	if err != nil {
		return err
	}
	return stage.ErrCancelled
}

func inFlightUnit(ctx context.Context, w ports.Watcher, units []*unit) (*unit, bool, error) {
	for _, u := range units {
		applied, settled, err := rollState(ctx, w, []*unit{u})
		if err != nil {
			return nil, false, err
		}
		if applied && !settled {
			return u, true, nil
		}
	}
	return nil, false, nil
}

// target is what the rollout applies, read at the pin commit.
type target struct {
	objs   ports.Objects // the ManifestDir build, as built
	layout layout
	routes ports.Objects // the StatusRoutes file, managed objects only
}

func loadTarget(ctx context.Context, rc *stage.RunCtx) (target, error) {
	ref := pinRef(rc.View())
	objs, err := build(ctx, rc, rc.Deps.Config.ManifestDir, ref)
	if err != nil {
		return target{}, err
	}
	routes, err := statusRoutes(ctx, rc, ref)
	if err != nil {
		return target{}, err
	}
	return target{objs: objs, layout: arrange(objs), routes: managedOnly(routes)}, nil
}

type rollRun struct{ rc *stage.RunCtx }

// steps is the apply order: priority classes before any pod that names one
// (the API server rejects such a pod), the shared network policies and
// middlewares the services' pods and routes rely on, every service, then
// the db status routes, whose backends are the services just rolled.
func (r rollRun) steps(t target) []step {
	var steps []step
	steps = r.batch(steps, step{key: "priority", label: "Priority classes"}, t.layout.priority)
	steps = r.batch(steps, step{key: "shared", label: "Shared policies and routes"}, t.layout.shared)
	for _, u := range t.layout.units {
		steps = append(steps, r.service(u))
	}
	return r.batch(steps, step{key: "status-routes", label: "Status routes"}, t.routes)
}

// batch appends s applying objs in one call; no step when objs is empty.
func (r rollRun) batch(steps []step, s step, objs ports.Objects) []step {
	if len(objs) == 0 {
		return steps
	}
	s.run = func(ctx context.Context) error {
		_, err := r.rc.Deps.Applier.Apply(ctx, objs)
		return applyErr(s.label, err)
	}
	return append(steps, s)
}

// service applies the unit's objects, then waits for every workload in it.
// The deployer's own unit lives in ops, outside the allowlist, and is
// listed skipped so the page says it was not rolled.
func (r rollRun) service(u *unit) step {
	s := step{key: u.name, label: u.name, last: &deploy.Item{}}
	s.run = func(ctx context.Context) error {
		if !u.managed {
			return skip(fmt.Sprintf("namespace %s is outside the apply allowlist; roll it by hand", u.ns))
		}
		if _, err := r.rc.Deps.Applier.Apply(ctx, u.objs); err != nil {
			return applyErr(u.name, err)
		}
		return r.wait(ctx, u, s.last)
	}
	return s
}

// wait bounds the whole service by RolloutTimeout, not each workload in it:
// the budget is per service. Rows are published on the parent context so
// the last one still lands after the bound expires.
func (r rollRun) wait(ctx context.Context, u *unit, last *deploy.Item) error {
	bounded, cancel := context.WithTimeout(ctx, r.rc.Deps.Config.RolloutTimeout)
	defer cancel()
	report := func(it deploy.Item) {
		it.Key, it.Label = u.name, u.name
		*last = it
		r.rc.SetItem(ctx, it)
	}
	pins := u.pins(r.rc.Deps.Config.ImageRepo)
	for _, w := range u.workloads {
		spec := ports.RolloutSpec{Workload: workloadRef(w), Pins: pins}
		if err := r.rc.Deps.Watcher.WaitRollout(bounded, spec, report); err != nil {
			return waitErr(ctx, u.name, err)
		}
	}
	return nil
}

// waitErr keeps the watcher's Fail, which carries the pod events and log
// tail, and a parent that ended (shutdown is not the service's fault).
// Anything else is a failed read of the cluster.
func waitErr(parent context.Context, name string, err error) error {
	if _, ok := ports.AsFail(err); ok || parent.Err() != nil {
		return err
	}
	return ports.Failf(deploy.FailKube, "watch %s rollout: %v", name, err)
}

// rolledOut compares whole image references, tag included, container by
// container: a container added or dropped by the pin commit counts as not
// rolled.
func rolledOut(ctx context.Context, w ports.Watcher, units []*unit) (bool, error) {
	applied, settled, err := rollState(ctx, w, units)
	return applied && settled, err
}

// rollState reports whether the units' pinned images are applied and, when
// they are, whether every workload settled.
func rollState(ctx context.Context, w ports.Watcher, units []*unit) (applied, settled bool, err error) {
	refs := refsOf(units)
	live, err := w.LiveImages(ctx, refs)
	if err != nil {
		return false, false, ports.Failf(deploy.FailKube, "read live images: %v", err)
	}
	if !maps.Equal(wantImages(workloadsOf(units)), liveImages(live)) {
		return false, false, nil
	}
	bad, err := w.Settled(ctx, refs)
	if err != nil {
		return true, false, ports.Failf(deploy.FailKube, "read workload status: %v", err)
	}
	return true, len(bad) == 0, nil
}

// slotKey is one container of one workload.
type slotKey struct {
	workload  ports.WorkloadRef
	container string
}

func wantImages(workloads []*unstructured.Unstructured) map[slotKey]string {
	out := map[slotKey]string{}
	for _, w := range workloads {
		for _, c := range containersOf(w) {
			out[slotKey{workload: workloadRef(w), container: c.name}] = c.image
		}
	}
	return out
}

func liveImages(live []ports.LiveImage) map[slotKey]string {
	out := make(map[slotKey]string, len(live))
	for _, l := range live {
		out[slotKey{workload: l.Workload, container: l.Container}] = l.Image
	}
	return out
}
