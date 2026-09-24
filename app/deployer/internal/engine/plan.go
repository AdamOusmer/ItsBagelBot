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
	rollbackPickerReleases   = 10
	concurrentTagLookupLimit = 4
)

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
	rels, err := e.d.Stage.GitHub.Releases(ctx, rollbackPickerReleases)
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

func commitish(target string) deploy.SHA {
	if shaPattern.MatchString(target) {
		return deploy.SHA(target)
	}
	return ""
}

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

func (e *Engine) taggedImages(ctx context.Context, s snapshot, match func(deploy.Tag) bool) (map[deploy.ImageName]bool, error) {
	var mu sync.Mutex
	built := map[deploy.ImageName]bool{}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrentTagLookupLimit)
	for _, img := range s.images(e.repo()) {
		g.Go(func() error {
			tags, err := e.imageTags(gctx, img)
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

func (e *Engine) liveRelease(ctx context.Context) (deploy.Version, deploy.SHA, error) {
	s, err := e.snapshot(ctx)
	if err != nil {
		return "", "", err
	}
	return e.releaseOf(ctx, s.live)
}

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

type snapshot struct {
	main      deploy.SHA
	workloads []workload
	live      []ports.LiveImage
}

func (e *Engine) snapshot(ctx context.Context) (snapshot, error) {
	d := e.d.Stage
	main, err := d.GitHub.BranchHead(ctx, d.Config.MainBranch)
	if err != nil {
		return snapshot{}, err
	}
	objs, err := e.mainObjects(ctx, main)
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

func (s snapshot) runners(images map[deploy.ImageName]bool, r imageRepo) map[string]bool {
	out := map[string]bool{}
	for _, w := range s.workloads {
		if slices.ContainsFunc(w.imageNames(r), func(n deploy.ImageName) bool { return images[n] }) {
			out[w.ref.Name] = true
		}
	}
	return out
}

func (s snapshot) images(r imageRepo) []deploy.ImageName {
	var out []deploy.ImageName
	for _, w := range s.workloads {
		out = append(out, w.imageNames(r)...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

type workload struct {
	ref    ports.WorkloadRef
	images map[string]string
}

func (w workload) imageNames(r imageRepo) []deploy.ImageName {
	var out []deploy.ImageName
	for _, image := range w.images {
		if name, _, ok := r.parse(image); ok {
			out = append(out, name)
		}
	}
	return out
}

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

func nextVersion(v deploy.Version) deploy.Version {
	s, ok := parseVersion(v)
	if !ok {
		return ""
	}
	return deploy.Version("v" + strconv.Itoa(s[0]) + "." + strconv.Itoa(s[1]) + "." + strconv.Itoa(s[2]+1) + "-beta")
}
