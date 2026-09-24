// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/codec"
)

var testConfig = ports.Config{
	MainBranch: "main", ImageRepo: "ghcr.io/o/r", ManifestDir: "deploy/k8s",
	HeartbeatEvery: 5 * time.Millisecond, LockTTL: time.Minute,
}

var testServices = []string{"users", "gossip", "console-admin", "deployer"}

var errOffline = errors.New("offline")

type memStore struct {
	mu                               sync.Mutex
	rev                              ports.Revision
	runs                             map[deploy.RunID]storedRun
	active                           deploy.RunID
	lock                             ports.Lock
	puts                             int
	beats                            int
	lostAcks, lostBeatAcks, downPuts int
}

type storedRun struct {
	raw []byte
	rev ports.Revision
}

func newMemStore() *memStore { return &memStore{runs: map[deploy.RunID]storedRun{}} }

func (s *memStore) Get(_ context.Context, id deploy.RunID) (deploy.Run, ports.Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(id)
}

func (s *memStore) load(id deploy.RunID) (deploy.Run, ports.Revision, error) {
	st, ok := s.runs[id]
	if !ok {
		return deploy.Run{}, 0, fmt.Errorf("%w: run %s", ports.ErrNotFound, id)
	}
	var run deploy.Run
	return run, st.rev, codec.Unmarshal(st.raw, &run)
}

func (s *memStore) Put(_ context.Context, run *deploy.Run, rev ports.Revision) (ports.Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.downPuts > 0 {
		s.downPuts--
		return 0, errOffline
	}
	if s.runs[run.ID].rev != rev {
		return 0, ports.ErrConflict
	}
	raw, err := codec.Marshal(run)
	if err != nil {
		return 0, err
	}
	s.rev++
	s.puts++
	s.runs[run.ID] = storedRun{raw: raw, rev: s.rev}
	s.track(run)
	return s.rev, s.lostAck(&s.lostAcks)
}

func (s *memStore) lostAck(n *int) error {
	if *n == 0 {
		return nil
	}
	*n--
	return errOffline
}

func (s *memStore) inject(f func(*memStore)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

func (s *memStore) beatCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beats
}

func (s *memStore) track(run *deploy.Run) {
	switch {
	case !run.State.Terminal():
		s.active = run.ID
	case s.active == run.ID:
		s.active = ""
	}
}

func (s *memStore) Active(_ context.Context) (deploy.Run, ports.Revision, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == "" {
		return deploy.Run{}, 0, false, nil
	}
	run, rev, err := s.load(s.active)
	return run, rev, err == nil, err
}

func (s *memStore) List(_ context.Context, limit int) ([]deploy.RunSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []deploy.RunSummary{}
	for id := range s.runs {
		run, _, err := s.load(id)
		if err != nil {
			return nil, err
		}
		out = append(out, run.Summary())
	}
	slices.SortFunc(out, func(a, b deploy.RunSummary) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out[:min(limit, len(out))], nil
}

func (s *memStore) AcquireLock(_ context.Context, owner deploy.RunID) (ports.Lock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock.Owner != "" && s.lock.Owner != owner {
		return ports.Lock{}, ports.ErrLockHeld
	}
	s.rev++
	s.lock = ports.Lock{Owner: owner, Revision: s.rev}
	return s.lock, nil
}

func (s *memStore) Heartbeat(_ context.Context, lock ports.Lock) (ports.Lock, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lock != s.lock {
		return ports.Lock{}, ports.ErrLockHeld
	}
	s.rev++
	s.beats++
	s.lock.Revision = s.rev
	return s.lock, s.lostAck(&s.lostBeatAcks)
}

func (s *memStore) ReleaseLock(_ context.Context, lock ports.Lock) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lock == s.lock {
		s.lock = ports.Lock{}
	}
	return nil
}

func (s *memStore) LockOwner(_ context.Context) (deploy.RunID, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lock.Owner, s.lock.Owner != "", nil
}

func (s *memStore) holdLock(owner deploy.RunID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rev++
	s.lock = ports.Lock{Owner: owner, Revision: s.rev}
}

func (s *memStore) putCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts
}

type memEvents struct {
	mu   sync.Mutex
	seqs []uint64
}

func (m *memEvents) Publish(_ context.Context, run *deploy.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seqs = append(m.seqs, run.Seq)
	return nil
}

func (m *memEvents) published() []uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.seqs)
}

type body func(ctx context.Context, rc *stage.RunCtx) error

type script struct {
	mu     sync.Mutex
	ran    []deploy.StageID
	done   map[deploy.StageID]bool
	bodies map[deploy.StageID]body
}

func newScript() *script {
	return &script{done: map[deploy.StageID]bool{}, bodies: map[deploy.StageID]body{}}
}

func (s *script) set(id deploy.StageID, b body) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies[id] = b
}

func (s *script) markDone(id deploy.StageID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done[id] = true
}

func (s *script) calls() []deploy.StageID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.ran)
}

func (s *script) stages() []stage.Stage {
	var out []stage.Stage
	for _, id := range deploy.StagesFor(deploy.KindRelease) {
		out = append(out, scripted{id: id, s: s})
	}
	return out
}

type scripted struct {
	id deploy.StageID
	s  *script
}

func (st scripted) ID() deploy.StageID { return st.id }

func (st scripted) Done(context.Context, *stage.RunCtx) (bool, error) {
	st.s.mu.Lock()
	defer st.s.mu.Unlock()
	return st.s.done[st.id], nil
}

func (st scripted) Run(ctx context.Context, rc *stage.RunCtx) error {
	st.s.mu.Lock()
	st.s.ran = append(st.s.ran, st.id)
	b := st.s.bodies[st.id]
	st.s.mu.Unlock()
	if b == nil {
		return nil
	}
	return b(ctx, rc)
}

type fakeGitHub struct {
	ports.GitHub
	main     deploy.SHA
	tags     map[deploy.Version]deploy.SHA
	releases []ports.Release
	commits  []deploy.Commit
	prs      []deploy.PRInfo
	reruns   []int64
	offMain  deploy.SHA
}

func (g *fakeGitHub) WorkflowRun(_ context.Context, id int64) (ports.WorkflowRun, error) {
	return ports.WorkflowRun{ID: id, Attempt: 1}, nil
}

func (g *fakeGitHub) RerunFailedJobs(_ context.Context, runID int64) error {
	g.reruns = append(g.reruns, runID)
	return nil
}

func (g *fakeGitHub) BranchHead(context.Context, ports.Branch) (deploy.SHA, error) {
	if g.main == "" {
		return "", errOffline
	}
	return g.main, nil
}

func (g *fakeGitHub) Tree(context.Context, ports.FilePath, ports.Ref) (ports.Files, error) {
	return ports.Files{}, nil
}

func (g *fakeGitHub) Tag(_ context.Context, name deploy.Version) (ports.TagRef, bool, error) {
	sha, ok := g.tags[name]
	return ports.TagRef{Name: name, CommitSHA: sha}, ok, nil
}

func (g *fakeGitHub) LatestTag(context.Context) (ports.TagRef, error) {
	var best ports.TagRef
	bestV := semver{-1, -1, -1}
	for name, sha := range g.tags {
		if v, _ := parseVersion(name); bestV.less(v) {
			best, bestV = ports.TagRef{Name: name, CommitSHA: sha}, v
		}
	}
	if best.Name == "" {
		return best, ports.ErrNotFound
	}
	return best, nil
}

func (g *fakeGitHub) Compare(_ context.Context, _, head ports.Ref) (ports.Comparison, error) {
	if g.offMain != "" && head == ports.Ref(g.offMain) {
		return ports.Comparison{AheadBy: 1}, nil
	}
	return ports.Comparison{Commits: g.commits}, nil
}

func (g *fakeGitHub) OpenPRs(context.Context) ([]deploy.PRInfo, error) { return g.prs, nil }

func (g *fakeGitHub) Release(_ context.Context, tag deploy.Version) (ports.Release, bool, error) {
	i := slices.IndexFunc(g.releases, func(r ports.Release) bool { return r.Tag == tag })
	if i < 0 {
		return ports.Release{}, false, nil
	}
	return g.releases[i], true, nil
}

func (g *fakeGitHub) Releases(context.Context, int) ([]ports.Release, error) { return g.releases, nil }

type fakeApplier struct {
	ports.Applier
	objs ports.Objects
}

func (a fakeApplier) Build(context.Context, ports.BuildSpec) (ports.Objects, error) {
	return a.objs, nil
}

type fakeWatcher struct {
	ports.Watcher
	live []ports.LiveImage
}

func (w fakeWatcher) LiveImages(context.Context, []ports.WorkloadRef) ([]ports.LiveImage, error) {
	return w.live, nil
}

type fakeRegistry struct {
	ports.Registry
	tags map[deploy.ImageName][]deploy.Tag
}

func (r fakeRegistry) Tags(_ context.Context, img deploy.ImageName) ([]deploy.Tag, error) {
	return r.tags[img], nil
}

var owner = deploy.Actor{ID: "1", Login: "owner"}

type harness struct {
	t      *testing.T
	eng    *Engine
	store  *memStore
	events *memEvents
	script *script
	gh     *fakeGitHub
	stop   context.CancelFunc
	exited chan error
	once   sync.Once
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, store: newMemStore(), events: &memEvents{}, script: newScript(), gh: &fakeGitHub{}}
	h.eng = New(Deps{
		Store: h.store, Events: h.events, Stages: h.script.stages(),
		Stage:    stage.Deps{GitHub: h.gh, Clock: ports.SystemClock{}, Config: testConfig, Log: zap.NewNop()},
		Services: testServices, Log: zap.NewNop(),
	})
	h.eng.endRetry = time.Millisecond
	return h
}

func (h *harness) boot() *harness {
	ctx, stop := context.WithCancel(context.Background())
	h.stop, h.exited = stop, make(chan error, 1)
	go func() { h.exited <- h.eng.Run(ctx) }()
	h.t.Cleanup(h.shutdown)
	return h
}

func (h *harness) shutdown() {
	h.once.Do(func() {
		h.stop()
		require.NoError(h.t, <-h.exited)
	})
}

func (h *harness) start(req deploy.StartRequest) deploy.Run {
	run, err := h.eng.Start(context.Background(), owner, req)
	require.NoError(h.t, err)
	return run
}

func (h *harness) stored(id deploy.RunID) deploy.Run {
	run, _, err := h.store.Get(context.Background(), id)
	require.NoError(h.t, err)
	return run
}

func (h *harness) until(id deploy.RunID, ok func(deploy.Run) bool) deploy.Run {
	h.t.Helper()
	var run deploy.Run
	require.Eventually(h.t, func() bool {
		run = h.stored(id)
		return ok(run)
	}, 5*time.Second, time.Millisecond, "run %s never reached the expected state", id)
	return run
}

func (h *harness) await(id deploy.RunID, state deploy.RunState) deploy.Run {
	h.t.Helper()
	return h.until(id, func(r deploy.Run) bool { return r.State == state && h.eng.executing(id) == nil })
}

func entered(ch chan<- struct{}, next body) body {
	return func(ctx context.Context, rc *stage.RunCtx) error {
		close(ch)
		return next(ctx, rc)
	}
}

func stageStates(run deploy.Run) map[deploy.StageID]deploy.StageState {
	out := make(map[deploy.StageID]deploy.StageState, len(run.Stages))
	for _, s := range run.Stages {
		out[s.ID] = s.State
	}
	return out
}

func statesFrom(ids []deploy.StageID, state deploy.StageState) map[deploy.StageID]deploy.StageState {
	out := make(map[deploy.StageID]deploy.StageState, len(ids))
	for _, id := range ids {
		out[id] = state
	}
	return out
}
