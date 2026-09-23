// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const (
	// planReleases is how many releases the rollback picker offers. Older
	// ones pin images a long-running cluster has moved far past; rolling
	// back that far is a hand operation.
	planReleases = 10
	// tagLookups bounds the concurrent registry tag listings a bump plan
	// makes, one per first-party image (16 today).
	tagLookups = 4
)

// Plan gathers what the Deploys page shows before a run. The reads are
// independent, so they run side by side, each filling its own fields.
func (e *Engine) Plan(ctx context.Context, _ deploy.Actor, req deploy.PlanRequest) (deploy.Plan, error) {
	var p deploy.Plan
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return e.planCluster(gctx, &p, req) })
	g.Go(func() error { return e.planHistory(gctx, &p) })
	g.Go(func() error { return e.planReleases(gctx, &p) })
	g.Go(func() (err error) { p.PRs, err = e.d.Stage.GitHub.OpenPRs(gctx); return err })
	g.Go(func() error { return e.planActive(gctx, &p) })
	if err := g.Wait(); err != nil {
		return deploy.Plan{}, err
	}
	return p, nil
}

// planCluster compares main's pins with what runs and names the release
// that runs and the services the requested run would roll.
func (e *Engine) planCluster(ctx context.Context, p *deploy.Plan, req deploy.PlanRequest) error {
	s, err := e.snapshot(ctx)
	if err != nil {
		return err
	}
	p.MainSHA, p.Drift = s.main, drift(s.workloads, s.live)
	p.InSync = len(p.Drift) == 0
	if p.LiveVersion, p.LiveSHA, err = e.releaseOf(ctx, s.live); err != nil {
		return err
	}
	p.Services, err = e.planServices(ctx, req, s)
	return err
}

// planHistory reads the last release tag and what main gained since. A
// repository without a release tag yet plans from nothing.
func (e *Engine) planHistory(ctx context.Context, p *deploy.Plan) error {
	gh := e.d.Stage.GitHub
	tag, err := gh.LatestTag(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	p.LastTag, p.NextVersion = tag.Name, nextVersion(tag.Name)
	cmp, err := gh.Compare(ctx, ports.Ref(tag.Name), ports.Ref(e.d.Stage.Config.MainBranch))
	p.Commits = cmp.Commits
	return err
}

func (e *Engine) planReleases(ctx context.Context, p *deploy.Plan) error {
	rels, err := e.d.Stage.GitHub.Releases(ctx, planReleases)
	for _, r := range rels {
		p.Releases = append(p.Releases, deploy.ReleaseInfo{
			Version: r.Tag, SHA: commitish(r.TargetCommitish), URL: r.URL, PublishedAt: r.PublishedAt,
		})
	}
	return err
}

func (e *Engine) planActive(ctx context.Context, p *deploy.Plan) error {
	run, _, ok, err := e.d.Store.Active(ctx)
	if ok {
		p.ActiveRunID = run.ID
	}
	return err
}

// commitish keeps a release target that is a commit. A branch name there
// ("main") says nothing about which commit the release was cut from.
func commitish(target string) deploy.SHA {
	if shaPattern.MatchString(target) {
		return deploy.SHA(target)
	}
	return ""
}

// planServices is every rollout unit in rollout order, or only those that
// run an image carrying the tag the run pins (see serviceTag).
func (e *Engine) planServices(ctx context.Context, req deploy.PlanRequest, s snapshot) ([]string, error) {
	match := serviceTag(req, s.main)
	if match == nil {
		return slices.Clone(e.d.Services), nil
	}
	built, err := e.taggedImages(ctx, s, match)
	running := s.runners(built, e.repo())
	var out []string
	for _, name := range e.d.Services {
		if running[name] {
			out = append(out, name)
		}
	}
	return out, err
}

// serviceTag picks the tag that marks an image as pinned by the run, or nil
// when the run rolls every service. A bump pins the main-<ts>-<sha12> tag
// of main's head: publish-images on a main push builds only what changed,
// so the tag's presence is what "this image changed" means. A rollback pins
// the target release's tag and leaves images without that build on their
// current pin, so only services with such a build change.
func serviceTag(req deploy.PlanRequest, main deploy.SHA) func(deploy.Tag) bool {
	switch {
	case req.Kind == deploy.KindBump:
		suffix := "-" + string(main[:min(len(main), 12)])
		return func(t deploy.Tag) bool { return mainBuild(t, suffix) }
	case req.Kind == deploy.KindRollback && req.RollbackTo != "":
		return func(t deploy.Tag) bool { return string(t) == string(req.RollbackTo) }
	}
	return nil
}

// taggedImages returns, per first-party image main pins, whether any of its
// registry tags matches.
func (e *Engine) taggedImages(ctx context.Context, s snapshot, match func(deploy.Tag) bool) (map[deploy.ImageName]bool, error) {
	var mu sync.Mutex
	built := map[deploy.ImageName]bool{}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(tagLookups)
	for _, img := range s.images(e.repo()) {
		g.Go(func() error {
			tags, err := e.d.Stage.Registry.Tags(gctx, img)
			mu.Lock()
			built[img] = slices.ContainsFunc(tags, match)
			mu.Unlock()
			return err
		})
	}
	return built, g.Wait()
}

func mainBuild(t deploy.Tag, suffix string) bool {
	return strings.HasPrefix(string(t), "main-") && strings.HasSuffix(string(t), suffix)
}

// liveRelease is the release the cluster runs and its tag's commit.
func (e *Engine) liveRelease(ctx context.Context) (deploy.Version, deploy.SHA, error) {
	s, err := e.snapshot(ctx)
	if err != nil {
		return "", "", err
	}
	return e.releaseOf(ctx, s.live)
}

// releaseOf picks the newest vX.Y.Z-beta tag among the first-party images
// that run. A bump leaves main-<ts>-<sha> tags beside a release's, and a
// rollback runs an older release throughout, so the newest release tag is
// the base the cluster was last released from; its tag commit is an
// ancestor of every later pin commit, which keeps the acl diff a superset.
func (e *Engine) releaseOf(ctx context.Context, live []ports.LiveImage) (deploy.Version, deploy.SHA, error) {
	v := newestRelease(live, e.repo())
	if v == "" {
		return "", "", nil
	}
	tag, ok, err := e.d.Stage.GitHub.Tag(ctx, v)
	if err != nil || !ok {
		return v, "", err
	}
	return v, tag.CommitSHA, nil
}

func (e *Engine) repo() imageRepo { return imageRepo(e.d.Stage.Config.ImageRepo) }

// snapshot is main's manifests beside what the cluster runs.
type snapshot struct {
	main      deploy.SHA
	workloads []workload
	live      []ports.LiveImage
}

// snapshot builds main's manifests the way the rollout will (namespaces
// come from the kustomization, not the files) and reads the live images of
// every workload they declare.
func (e *Engine) snapshot(ctx context.Context) (snapshot, error) {
	d := e.d.Stage
	main, err := d.GitHub.BranchHead(ctx, d.Config.MainBranch)
	if err != nil {
		return snapshot{}, err
	}
	files, err := d.GitHub.Tree(ctx, d.Config.ManifestDir, ports.Ref(main))
	if err != nil {
		return snapshot{}, err
	}
	objs, err := d.Applier.Build(ctx, ports.BuildSpec{Files: files, Root: d.Config.ManifestDir})
	if err != nil {
		return snapshot{}, err
	}
	wls := workloadsOf(objs)
	refs := make([]ports.WorkloadRef, len(wls))
	for i, w := range wls {
		refs[i] = w.ref
	}
	live, err := d.Watcher.LiveImages(ctx, refs)
	return snapshot{main: main, workloads: wls, live: live}, err
}

// runners names the workloads that run one of images.
func (s snapshot) runners(images map[deploy.ImageName]bool, r imageRepo) map[string]bool {
	out := map[string]bool{}
	for _, w := range s.workloads {
		if slices.ContainsFunc(w.imageNames(r), func(n deploy.ImageName) bool { return images[n] }) {
			out[w.ref.Name] = true
		}
	}
	return out
}

// images lists the first-party images main pins, each once.
func (s snapshot) images(r imageRepo) []deploy.ImageName {
	var out []deploy.ImageName
	for _, w := range s.workloads {
		out = append(out, w.imageNames(r)...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// workload is one pod-running object in main's manifests and the image
// each of its containers pins.
type workload struct {
	ref    ports.WorkloadRef
	images map[string]string
}

// imageNames are the first-party images the workload's containers pin.
func (w workload) imageNames(r imageRepo) []deploy.ImageName {
	var out []deploy.ImageName
	for _, image := range w.images {
		if name, _, ok := r.parse(image); ok {
			out = append(out, name)
		}
	}
	return out
}

// podSpecs is where each pod-running kind keeps its pod spec.
var podSpecs = map[string][]string{
	"Deployment": {"spec", "template", "spec"},
	"DaemonSet":  {"spec", "template", "spec"},
	"CronJob":    {"spec", "jobTemplate", "spec", "template", "spec"},
}

func workloadsOf(objs ports.Objects) []workload {
	var out []workload
	for _, o := range objs {
		if w, ok := workloadOf(o); ok {
			out = append(out, w)
		}
	}
	return out
}

func workloadOf(o *unstructured.Unstructured) (workload, bool) {
	spec, ok := podSpecs[o.GetKind()]
	if !ok {
		return workload{}, false
	}
	images := map[string]string{}
	for _, list := range []string{"initContainers", "containers"} {
		containers, _, _ := unstructured.NestedSlice(o.Object, slices.Concat(spec, []string{list})...)
		addImages(images, containers)
	}
	return workload{
		ref:    ports.WorkloadRef{Kind: o.GetKind(), Namespace: ports.Namespace(o.GetNamespace()), Name: o.GetName()},
		images: images,
	}, true
}

func addImages(images map[string]string, containers []any) {
	for _, c := range containers {
		m, _ := c.(map[string]any)
		name, _ := m["name"].(string)
		image, _ := m["image"].(string)
		images[name] = image
	}
}

// drift lists every running container whose image is not main's pin. A
// container main does not declare is not drift: nothing pins it.
func drift(wls []workload, live []ports.LiveImage) []deploy.DriftItem {
	pinned := make(map[ports.WorkloadRef]map[string]string, len(wls))
	for _, w := range wls {
		pinned[w.ref] = w.images
	}
	var out []deploy.DriftItem
	for _, l := range live {
		want, ok := pinned[l.Workload][l.Container]
		if ok && want != l.Image {
			out = append(out, deploy.DriftItem{
				Namespace: string(l.Workload.Namespace), Workload: l.Workload.Name,
				Container: l.Container, Pinned: want, Live: l.Image,
			})
		}
	}
	return out
}

// imageRepo parses first-party references, <repo>/<name>:<tag>@<digest>.
type imageRepo string

func (r imageRepo) parse(image string) (deploy.ImageName, deploy.Tag, bool) {
	rest, ok := strings.CutPrefix(image, string(r)+"/")
	rest, _, _ = strings.Cut(rest, "@")
	name, tag, found := strings.Cut(rest, ":")
	return deploy.ImageName(name), deploy.Tag(tag), ok && found
}

func newestRelease(live []ports.LiveImage, r imageRepo) deploy.Version {
	var best deploy.Version
	bestV := semver{-1, -1, -1}
	for _, l := range live {
		_, tag, _ := r.parse(l.Image)
		v, ok := parseVersion(deploy.Version(tag))
		if ok && bestV.less(v) {
			best, bestV = deploy.Version(tag), v
		}
	}
	return best
}

type semver [3]int

func parseVersion(v deploy.Version) (semver, bool) {
	m := versionPattern.FindStringSubmatch(string(v))
	if m == nil {
		return semver{}, false
	}
	var s semver
	for i := range s {
		s[i], _ = strconv.Atoi(m[i+1])
	}
	return s, true
}

func (s semver) less(o semver) bool { return slices.Compare(s[:], o[:]) < 0 }

// nextVersion bumps the patch: the train cuts patch releases, and a minor
// bump is the operator typing it.
func nextVersion(v deploy.Version) deploy.Version {
	s, ok := parseVersion(v)
	if !ok {
		return ""
	}
	return deploy.Version("v" + strconv.Itoa(s[0]) + "." + strconv.Itoa(s[1]) + "." + strconv.Itoa(s[2]+1) + "-beta")
}
