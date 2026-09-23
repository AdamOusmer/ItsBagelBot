// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cluster

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const testRepo = "ghcr.io/adamousmer/itsbagelbot"

var t0 = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

// fakeSink is the engine side of a RunCtx: one run, edited in place.
type fakeSink struct {
	mu  sync.Mutex
	run deploy.Run
}

func newSink(kind deploy.RunKind) *fakeSink {
	run := deploy.Run{ID: "r1", Kind: kind, State: deploy.RunRunning, TargetSHA: "target",
		Outputs: deploy.Outputs{LiveSHA: "live", PinSHA: "pin"}}
	for _, id := range deploy.StagesFor(kind) {
		run.Stages = append(run.Stages, deploy.Stage{ID: id, State: deploy.StatePending})
	}
	return &fakeSink{run: run}
}

func (s *fakeSink) View() deploy.Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.run
}

func (s *fakeSink) Update(_ context.Context, edit func(*deploy.Run)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	edit(&s.run)
	s.run.Seq++
	return nil
}

func (s *fakeSink) AwaitApproval(context.Context, deploy.StageID, string) error { return nil }

func (s *fakeSink) cancel() {
	_ = s.Update(context.Background(), func(r *deploy.Run) { r.CancelRequested = true })
}

// rows is the stage's rows as "state done/total" by key.
func (s *fakeSink) rows(id deploy.StageID) map[string]string {
	run := s.View()
	out := map[string]string{}
	for _, it := range run.Stage(id).Items {
		out[it.Key] = fmt.Sprintf("%s %d/%d", it.State, it.Progress.Done, it.Progress.Total)
	}
	return out
}

type fakeGitHub struct {
	ports.GitHub
	changed []ports.FilePath // Compare's files
}

func (g *fakeGitHub) Tree(_ context.Context, dir ports.FilePath, _ ports.Ref) (ports.Files, error) {
	return ports.Files{dir + "/kustomization.yaml": nil}, nil
}

func (g *fakeGitHub) File(_ context.Context, path ports.FilePath, _ ports.Ref) ([]byte, error) {
	return []byte(path), nil
}

func (g *fakeGitHub) Compare(context.Context, ports.Ref, ports.Ref) (ports.Comparison, error) {
	return ports.Comparison{Files: g.changed}, nil
}

// fakeApplier builds from a fixed set per root and records every Apply call
// as the "Kind/name" of each object in it.
type fakeApplier struct {
	builds  map[ports.FilePath]ports.Objects
	changed []ports.ObjectRef
	applied [][]string
}

func (a *fakeApplier) Build(_ context.Context, spec ports.BuildSpec) (ports.Objects, error) {
	return a.builds[spec.Root], nil
}

func (a *fakeApplier) Lint(ports.Objects) []ports.LintFinding { return nil }

func (a *fakeApplier) Apply(_ context.Context, objs ports.Objects) (ports.ApplyResult, error) {
	names := make([]string, len(objs))
	for i, o := range objs {
		names[i] = o.GetKind() + "/" + o.GetName()
	}
	a.applied = append(a.applied, names)
	return ports.ApplyResult{Changed: a.changed}, nil
}

type fakeWatcher struct {
	ports.Watcher
	waited    []string
	afterWait func(n int)
	servers   []ports.NATSServer
	live      []ports.LiveImage
	unsettled []ports.Unsettled
	off       map[string][]ports.Mismatch // VerifyImageIDs answer by workload name
	codes     map[ports.URL]int
}

func (w *fakeWatcher) WaitRollout(_ context.Context, spec ports.RolloutSpec, progress ports.ProgressFunc) error {
	progress(deploy.Item{State: deploy.StateRunning, Progress: deploy.Progress{Done: 1, Total: 1},
		Nodes: []deploy.NodePod{{Node: "node1", Pod: spec.Workload.Name + "-0", Phase: deploy.PodNew}}})
	w.waited = append(w.waited, spec.Workload.Name)
	if w.afterWait != nil {
		w.afterWait(len(w.waited))
	}
	return nil
}

func (w *fakeWatcher) NATSServers(context.Context) ([]ports.NATSServer, error) { return w.servers, nil }

// LiveImages answers for the asked workloads only, as the real watcher does.
func (w *fakeWatcher) LiveImages(_ context.Context, refs []ports.WorkloadRef) ([]ports.LiveImage, error) {
	var out []ports.LiveImage
	for _, l := range w.live {
		if slices.Contains(refs, l.Workload) {
			out = append(out, l)
		}
	}
	return out, nil
}

func (w *fakeWatcher) Settled(context.Context, []ports.WorkloadRef) ([]ports.Unsettled, error) {
	return w.unsettled, nil
}

func (w *fakeWatcher) VerifyImageIDs(_ context.Context, refs []ports.WorkloadRef, _ ports.Pins) ([]ports.Mismatch, error) {
	return w.off[refs[0].Name], nil
}

func (w *fakeWatcher) Probe(_ context.Context, url ports.URL) (int, error) {
	code, ok := w.codes[url]
	if !ok {
		return 0, errors.New("connection refused")
	}
	return code, nil
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return t0 }

func testConfig() ports.Config {
	return ports.Config{
		ImageRepo:       testRepo,
		ManifestDir:     "deploy/k8s",
		MessagingDir:    "deploy/messaging",
		PriorityClasses: "deploy/k8s/priorityclasses.yaml",
		StatusRoutes:    "deploy/db/status-routes.yaml",
		RolloutTimeout:  time.Minute,
		ACLTimeout:      30 * time.Millisecond,
		PollEvery:       time.Millisecond,
	}
}

// harness is one stage's RunCtx over the fakes.
type harness struct {
	sink    *fakeSink
	gh      *fakeGitHub
	applier *fakeApplier
	watcher *fakeWatcher
}

func newHarness(kind deploy.RunKind) *harness {
	return &harness{
		sink:    newSink(kind),
		gh:      &fakeGitHub{},
		applier: &fakeApplier{builds: map[ports.FilePath]ports.Objects{}},
		watcher: &fakeWatcher{},
	}
}

func (h *harness) rc(id deploy.StageID) *stage.RunCtx {
	deps := stage.Deps{GitHub: h.gh, Applier: h.applier, Watcher: h.watcher, Clock: fixedClock{},
		Config: testConfig(), Log: zap.NewNop()}
	return stage.New(id, deps, h.sink)
}

func object(ref ports.ObjectRef) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{}}
	o.SetKind(ref.Kind)
	o.SetNamespace(string(ref.Namespace))
	o.SetName(ref.Name)
	return o
}

// service is a Deployment named after, and running, its own first-party
// image.
func service(ns ports.Namespace, name deploy.ImageName) *unstructured.Unstructured {
	o := object(ports.ObjectRef{Kind: kindDeployment, Namespace: ns, Name: string(name)})
	c := map[string]any{"name": string(name), "image": imageOf(name)}
	_ = unstructured.SetNestedSlice(o.Object, []any{c}, "spec", "template", "spec", "containers")
	return o
}

func imageOf(name deploy.ImageName) string {
	return testRepo + "/" + string(name) + ":v1@sha256:" + string(name)
}

func ingressRoute(ref ports.ObjectRef, matches ...string) *unstructured.Unstructured {
	o := object(ports.ObjectRef{Kind: kindIngressRoute, Namespace: ref.Namespace, Name: ref.Name})
	routes := make([]any, len(matches))
	for i, m := range matches {
		routes[i] = map[string]any{"match": m}
	}
	_ = unstructured.SetNestedSlice(o.Object, routes, "spec", "routes")
	return o
}

// errCode names an outcome for comparison: "" for nil, "skipped",
// "cancelled", a Fail's code, or the error text.
func errCode(err error) string {
	if f, ok := ports.AsFail(err); ok {
		return string(f.Code)
	}
	switch {
	case err == nil:
		return ""
	case errors.Is(err, stage.ErrSkipped):
		return "skipped"
	case errors.Is(err, stage.ErrCancelled):
		return "cancelled"
	}
	return err.Error()
}
