// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const testRepo = "ghcr.io/adamousmer/itsbagelbot"

func testConfig() ports.Config {
	return ports.Config{
		Owner: "AdamOusmer", Repo: "ItsBagelBot", MainBranch: "main", ImageRepo: testRepo,
		Workflow: "publish-images.yml", ManifestDir: "deploy/k8s", ChangelogDir: "web/marketing/src/content/changelog",
		UpdateBranchLimit: 3, LogTailLines: 50, PollEvery: time.Microsecond,
	}
}

type fakeSink struct {
	mu    sync.Mutex
	run   deploy.Run
	waits []string
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
	for _, st := range s.run.Stages {
		s.noteWait(st.Waiting)
	}
	return nil
}

func (s *fakeSink) noteWait(msg string) {
	if msg != "" && !slices.Contains(s.waits, msg) {
		s.waits = append(s.waits, msg)
	}
}

func (s *fakeSink) AwaitApproval(context.Context, deploy.StageID, string) error { return nil }

func newRun(kind deploy.RunKind) deploy.Run {
	run := deploy.Run{ID: "run1", Kind: kind, State: deploy.RunRunning}
	for _, id := range deploy.StagesFor(kind) {
		run.Stages = append(run.Stages, deploy.Stage{ID: id, State: deploy.StatePending})
	}
	return run
}

type fixture struct {
	gh   *fakeGitHub
	reg  *fakeRegistry
	sink *fakeSink
}

func newFixture(t *testing.T, run deploy.Run) *fixture {
	t.Helper()
	return &fixture{gh: newFakeGitHub(), reg: &fakeRegistry{images: map[ports.ImageRef]ports.ImageInfo{}}, sink: &fakeSink{run: run}}
}

func (f *fixture) rc(id deploy.StageID) *stage.RunCtx {
	return stage.New(id, stage.Deps{
		GitHub: f.gh, Registry: f.reg, Clock: fixedClock{}, Config: testConfig(), Log: zap.NewNop(),
	}, f.sink)
}

func (f *fixture) stageOf(id deploy.StageID) deploy.Stage {
	run := f.sink.View()
	return *run.Stage(id)
}

func (f *fixture) runStage(t *testing.T, id deploy.StageID) (done bool, err error) {
	t.Helper()
	s := stageByID(t, id)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rc := f.rc(id)
	if done, err = s.Done(ctx, rc); err != nil || done {
		return done, err
	}
	return false, s.Run(ctx, rc)
}

func stageByID(t *testing.T, id deploy.StageID) stage.Stage {
	t.Helper()
	for _, s := range All() {
		if s.ID() == id {
			return s
		}
	}
	t.Fatalf("no stage %s", id)
	return nil
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 9, 23, 23, 30, 0, 0, time.UTC) }

func loadManifests(t *testing.T) ports.Files {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("testdata", "k8s"))
	if err != nil {
		t.Fatal(err)
	}
	files := ports.Files{}
	for _, e := range entries {
		body, err := os.ReadFile(filepath.Join("testdata", "k8s", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[ports.FilePath("deploy/k8s/"+e.Name())] = body
	}
	return files
}

type fakePR struct {
	ports.PullRequest
	files  ports.Files
	behind int
}

type fakeGitHub struct {
	mu       sync.Mutex
	history  []deploy.SHA
	offMain  map[deploy.SHA]bool
	trees    map[deploy.SHA]ports.Files
	branches map[ports.Branch]deploy.SHA
	prs      map[int]*fakePR
	tags     map[deploy.Version]ports.TagRef
	releases map[deploy.Version]ports.Release
	notes    map[deploy.Version]string
	checks   map[deploy.SHA][]ports.CheckSummary
	runs     []ports.WorkflowRun
	steps    map[int64][]runStep
	logs     map[int64][]string
	attested map[deploy.Digest]bool
	calls    []string
	seq      int
}

type runStep struct {
	run  ports.WorkflowRun
	jobs []ports.Job
}

func newFakeGitHub() *fakeGitHub {
	return &fakeGitHub{
		history: []deploy.SHA{"base"}, trees: map[deploy.SHA]ports.Files{"base": {}},
		branches: map[ports.Branch]deploy.SHA{}, prs: map[int]*fakePR{}, offMain: map[deploy.SHA]bool{},
		tags: map[deploy.Version]ports.TagRef{}, releases: map[deploy.Version]ports.Release{},
		notes: map[deploy.Version]string{}, checks: map[deploy.SHA][]ports.CheckSummary{},
		steps: map[int64][]runStep{}, logs: map[int64][]string{}, attested: map[deploy.Digest]bool{},
	}
}

func (g *fakeGitHub) head() deploy.SHA { return g.history[len(g.history)-1] }

func (g *fakeGitHub) commitMain(sha deploy.SHA, files ports.Files) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.trees[sha] = overlay(g.trees[g.head()], files)
	g.history = append(g.history, sha)
}

func overlay(base, files ports.Files) ports.Files {
	out := maps.Clone(base)
	if out == nil {
		out = ports.Files{}
	}
	maps.Copy(out, files)
	return out
}

func (g *fakeGitHub) addPR(pr fakePR) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.prs[pr.Number] = &pr
}

func (g *fakeGitHub) log(format string, args ...any) {
	g.calls = append(g.calls, fmt.Sprintf(format, args...))
}

func (g *fakeGitHub) resolve(ref ports.Ref) deploy.SHA {
	if ref == "main" {
		return g.head()
	}
	return deploy.SHA(ref)
}

func (g *fakeGitHub) BranchHead(_ context.Context, b ports.Branch) (deploy.SHA, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.resolve(ports.Ref(b)), nil
}

func (g *fakeGitHub) LatestTag(context.Context) (ports.TagRef, error) {
	return ports.TagRef{}, ports.ErrNotImplemented
}

func (g *fakeGitHub) Tag(_ context.Context, name deploy.Version) (ports.TagRef, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.tags[name]
	return t, ok, nil
}

func (g *fakeGitHub) UpsertTag(_ context.Context, spec ports.TagSpec) (ports.TagRef, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	cur, ok := g.tags[spec.Name]
	moves := ok && cur.CommitSHA != spec.Commit
	if moves && !spec.Force {
		return ports.TagRef{}, ports.ErrConflict
	}
	g.log("tag %s -> %s force=%t", spec.Name, spec.Commit, spec.Force)
	g.tags[spec.Name] = ports.TagRef{Name: spec.Name, CommitSHA: spec.Commit, ObjectSHA: "obj-" + spec.Commit}
	return g.tags[spec.Name], nil
}

func (g *fakeGitHub) Compare(_ context.Context, base, head ports.Ref) (ports.Comparison, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.offMain[g.resolve(head)] {
		return ports.Comparison{AheadBy: 1, BehindBy: 1}, nil
	}
	b, h := slices.Index(g.history, g.resolve(base)), slices.Index(g.history, g.resolve(head))
	if b < 0 || h < 0 {
		return ports.Comparison{}, fmt.Errorf("compare %s...%s: %w", base, head, ports.ErrNotFound)
	}
	return ports.Comparison{AheadBy: max(h-b, 0), BehindBy: max(b-h, 0)}, nil
}

func (g *fakeGitHub) OpenPRs(context.Context) ([]deploy.PRInfo, error) { return nil, nil }

func (g *fakeGitHub) PR(_ context.Context, number int) (ports.PullRequest, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	pr, ok := g.prs[number]
	if !ok {
		return ports.PullRequest{}, ports.ErrNotFound
	}
	return pr.PullRequest, nil
}

func (g *fakeGitHub) FindPR(_ context.Context, head ports.Branch) (ports.PullRequest, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, n := range slices.Sorted(maps.Keys(g.prs)) {
		if pr := g.prs[n]; pr.HeadBranch == head {
			return pr.PullRequest, true, nil
		}
	}
	return ports.PullRequest{}, false, nil
}

func (g *fakeGitHub) CreatePR(_ context.Context, spec ports.PRSpec) (ports.PullRequest, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seq++
	n := 1000 + g.seq
	mergeable := true
	pr := &fakePR{PullRequest: ports.PullRequest{
		Number: n, Title: spec.Title, URL: fmt.Sprintf("https://github.test/pull/%d", n), HeadBranch: spec.Head,
		HeadSHA: g.branches[spec.Head], BaseBranch: spec.Base, Open: true, Mergeable: &mergeable,
	}, files: g.trees[g.branches[spec.Head]]}
	g.prs[n] = pr
	g.log("pr #%d %s: %s", n, spec.Head, spec.Title)
	return pr.PullRequest, nil
}

func (g *fakeGitHub) UpdateBranch(_ context.Context, number int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	pr := g.prs[number]
	pr.behind--
	pr.Behind = pr.behind > 0
	pr.HeadSHA += "u"
	g.log("update-branch #%d", number)
	return nil
}

func (g *fakeGitHub) SquashMerge(_ context.Context, number int, expectHead deploy.SHA) (deploy.SHA, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	pr := g.prs[number]
	if pr.HeadSHA != expectHead {
		return "", ports.ErrConflict
	}
	sha := deploy.SHA(fmt.Sprintf("merge%d", number))
	g.trees[sha] = overlay(g.trees[g.head()], pr.files)
	g.history = append(g.history, sha)
	pr.Merged, pr.Open, pr.MergeSHA = true, false, sha
	g.log("merge #%d", number)
	return sha, nil
}

func (g *fakeGitHub) DeleteBranch(_ context.Context, b ports.Branch) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.branches[b]; !ok {
		return ports.ErrNotFound
	}
	delete(g.branches, b)
	g.log("delete %s", b)
	return nil
}

func (g *fakeGitHub) CommitFiles(_ context.Context, spec ports.CommitSpec) (deploy.SHA, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.branches[spec.Branch]; ok {
		return "", ports.ErrConflict
	}
	g.seq++
	sha := deploy.SHA(fmt.Sprintf("commit%d", g.seq))
	g.trees[sha] = maps.Clone(spec.Files)
	g.branches[spec.Branch] = sha
	g.log("commit %s on %s from %s: %s", sha, spec.Branch, spec.Base, strings.Join(sortedFilePaths(spec.Files), ","))
	return sha, nil
}

func sortedFilePaths(files ports.Files) []string {
	out := make([]string, 0, len(files))
	for p := range files {
		out = append(out, string(p))
	}
	slices.Sort(out)
	return out
}

func (g *fakeGitHub) Checks(_ context.Context, sha deploy.SHA) (ports.CheckSummary, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	key := deploy.SHA(strings.TrimRight(string(sha), "u"))
	seq := g.checks[key]
	if len(seq) == 0 {
		return ports.CheckSummary{State: deploy.ChecksSuccess, CodeScene: deploy.ChecksSuccess}, nil
	}
	if len(seq) > 1 {
		g.checks[key] = seq[1:]
	}
	return seq[0], nil
}

func (g *fakeGitHub) File(_ context.Context, p ports.FilePath, ref ports.Ref) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	body, ok := g.trees[g.resolve(ref)][p]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return body, nil
}

func (g *fakeGitHub) Tree(_ context.Context, dir ports.FilePath, ref ports.Ref) (ports.Files, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := ports.Files{}
	for p, body := range g.trees[g.resolve(ref)] {
		if strings.HasPrefix(string(p), string(dir)+"/") {
			out[p] = body
		}
	}
	return out, nil
}

func (g *fakeGitHub) Release(_ context.Context, tag deploy.Version) (ports.Release, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r, ok := g.releases[tag]
	return r, ok, nil
}

func (g *fakeGitHub) UpsertRelease(_ context.Context, spec ports.ReleaseSpec) (ports.Release, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := ports.Release{
		ID: 7, Tag: spec.Tag, Title: spec.Title, URL: "https://github.test/releases/tag/" + string(spec.Tag),
		TargetCommitish: string(spec.Target), Latest: true,
	}
	g.releases[spec.Tag], g.notes[spec.Tag] = r, spec.Notes
	g.log("release %s at %s", spec.Tag, spec.Target)
	return r, nil
}

func (g *fakeGitHub) Releases(context.Context, int) ([]ports.Release, error) { return nil, nil }

func (g *fakeGitHub) FindWorkflowRun(_ context.Context, q ports.RunQuery) (ports.WorkflowRun, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, r := range g.runs {
		if r.HeadSHA == q.SHA {
			return g.current(r.ID).run, true, nil
		}
	}
	return ports.WorkflowRun{}, false, nil
}

func (g *fakeGitHub) current(id int64) runStep { return g.steps[id][0] }

func (g *fakeGitHub) WorkflowRun(_ context.Context, id int64) (ports.WorkflowRun, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if steps := g.steps[id]; len(steps) > 1 {
		g.steps[id] = steps[1:]
	}
	return g.current(id).run, nil
}

func (g *fakeGitHub) ActiveRuns(context.Context, ports.Workflow) ([]ports.WorkflowRun, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.runs), nil
}

func (g *fakeGitHub) RunJobs(_ context.Context, id int64) ([]ports.Job, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.current(id).jobs, nil
}

func (g *fakeGitHub) JobLogTail(_ context.Context, jobID int64, n int) ([]string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	lines := g.logs[jobID]
	return lines[max(len(lines)-n, 0):], nil
}

func (g *fakeGitHub) RerunFailedJobs(_ context.Context, id int64) error {
	g.log("rerun %d", id)
	return nil
}

func (g *fakeGitHub) AttestationExists(_ context.Context, d deploy.Digest) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.attested[d], nil
}

type fakeRegistry struct {
	images  map[ports.ImageRef]ports.ImageInfo
	tags    map[deploy.ImageName][]deploy.Tag
	refused map[ports.ImageRef]error
}

func (r *fakeRegistry) Resolve(_ context.Context, ref ports.ImageRef) (ports.ImageInfo, error) {
	if err := r.refused[ref]; err != nil {
		return ports.ImageInfo{}, err
	}
	info, ok := r.images[ref]
	if !ok {
		return ports.ImageInfo{}, fmt.Errorf("%s:%s: %w", ref.Image, ref.Tag, ports.ErrNotFound)
	}
	return info, nil
}

func (r *fakeRegistry) Tags(_ context.Context, img deploy.ImageName) ([]deploy.Tag, error) {
	return r.tags[img], nil
}

var bothArches = []string{"linux/amd64", "linux/arm64"}

func testDigest(seed string) deploy.Digest {
	hex := strings.Repeat(fmt.Sprintf("%x", seed), 64)
	return deploy.Digest("sha256:" + hex[:64])
}

func (f *fixture) publish(images []deploy.ImageName, tag deploy.Tag, rev deploy.SHA) map[deploy.ImageName]deploy.ImagePin {
	pins := map[deploy.ImageName]deploy.ImagePin{}
	for _, img := range images {
		d := testDigest(string(img) + string(tag))
		f.reg.images[ports.ImageRef{Image: img, Tag: tag}] = ports.ImageInfo{Digest: d, Platforms: bothArches, Revision: rev}
		f.gh.attested[d] = true
		pins[img] = deploy.ImagePin{Tag: tag, Digest: d}
	}
	return pins
}
