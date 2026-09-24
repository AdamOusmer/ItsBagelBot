// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// The cluster runs ARM and Intel nodes: a single-arch index fails to pull on half.
var requiredPlatforms = []string{"linux/amd64", "linux/arm64"}

func digestsDone(_ context.Context, rc *stage.RunCtx) (bool, error) {
	return len(rc.View().Outputs.Digests) > 0, nil
}

type digestPlan struct {
	refs        []ports.ImageRef
	revision    deploy.SHA
	current     map[deploy.ImageName]deploy.ImagePin
	keepMissing bool
}

func runDigests(ctx context.Context, rc *stage.RunCtx) error {
	run := rc.View()
	plan, err := planDigests(ctx, rc, run)
	if err != nil {
		return err
	}
	pins, err := resolveAll(ctx, rc, plan)
	if err != nil {
		return err
	}
	if run.Kind == deploy.KindBump {
		pins = changedOnly(ctx, rc, pins, plan.current)
	}
	if len(pins) == 0 {
		return fail(deploy.FailDigestRefused, "nothing to pin: no image built at %s differs from main's pins", short(plan.revision))
	}
	return rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.Digests = pins })
}

func planDigests(ctx context.Context, rc *stage.RunCtx, run deploy.Run) (digestPlan, error) {
	lines, err := mainPinLines(ctx, rc)
	if err != nil {
		return digestPlan{}, err
	}
	current := pinsByImage(lines)
	if run.Kind != deploy.KindBump {
		return digestPlan{refs: versionRefs(current, run.Version), revision: run.Outputs.TagSHA, current: current}, nil
	}
	refs, err := bumpRefs(ctx, rc, run, lines)
	return digestPlan{refs: refs, revision: run.TargetSHA, current: current}, err
}

func versionRefs(current map[deploy.ImageName]deploy.ImagePin, v deploy.Version) []ports.ImageRef {
	refs := make([]ports.ImageRef, 0, len(current))
	for _, img := range sortedImages(current) {
		refs = append(refs, ports.ImageRef{Image: img, Tag: deploy.Tag(v)})
	}
	return refs
}

func bumpRefs(ctx context.Context, rc *stage.RunCtx, run deploy.Run, lines []PinLine) ([]ports.ImageRef, error) {
	jobs, err := rc.Deps.GitHub.RunJobs(ctx, run.Outputs.BuildRunID)
	if err != nil {
		return nil, err
	}
	var refs []ports.ImageRef
	for _, img := range bumpImages(builtImages(jobs), lines, run.Services) {
		tag, err := mainTag(ctx, rc.Deps.Registry, img, run.TargetSHA)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ports.ImageRef{Image: img, Tag: tag})
	}
	return refs, nil
}

func bumpImages(built []deploy.ImageName, lines []PinLine, services []string) []deploy.ImageName {
	var out []deploy.ImageName
	for _, img := range built {
		if pinnedBy(lines, img, services) {
			out = append(out, img)
		}
	}
	return out
}

func pinnedBy(lines []PinLine, img deploy.ImageName, services []string) bool {
	for _, l := range lines {
		if l.Image == img && inScope(services, l.Workload) {
			return true
		}
	}
	return false
}

func inScope(services []string, workload string) bool {
	return len(services) == 0 || slices.Contains(services, workload)
}

func mainTag(ctx context.Context, reg ports.Registry, img deploy.ImageName, sha deploy.SHA) (deploy.Tag, error) {
	tags, err := reg.Tags(ctx, img)
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`^main-(\d+)-` + regexp.QuoteMeta(short(sha)) + `$`)
	var best deploy.Tag
	bestTS := int64(-1)
	for _, t := range tags {
		m := re.FindStringSubmatch(string(t))
		if m == nil {
			continue
		}
		if ts, _ := strconv.ParseInt(m[1], 10, 64); ts > bestTS {
			best, bestTS = t, ts
		}
	}
	if best == "" {
		return "", fail(deploy.FailDigestRefused, "%s has no main-<ts>-%s tag", img, short(sha))
	}
	return best, nil
}

func resolveAll(ctx context.Context, rc *stage.RunCtx, plan digestPlan) (map[deploy.ImageName]deploy.ImagePin, error) {
	items := make([]deploy.Item, len(plan.refs))
	for i, ref := range plan.refs {
		items[i] = deploy.Item{Key: string(ref.Image), Label: string(ref.Image), State: deploy.StatePending, Detail: string(ref.Tag)}
	}
	rc.SetItems(ctx, items)
	res := &resolution{rc: rc, plan: plan, pins: map[deploy.ImageName]deploy.ImagePin{}}
	for i, ref := range plan.refs {
		if err := res.check(ctx, ref); err != nil {
			return nil, err
		}
		rc.SetProgress(ctx, deploy.Progress{Done: i + 1, Total: len(plan.refs)})
	}
	if err := res.refusal(); err != nil {
		return nil, err
	}
	return res.pins, nil
}

type resolution struct {
	rc      *stage.RunCtx
	plan    digestPlan
	pins    map[deploy.ImageName]deploy.ImagePin
	refused []string
}

func (r *resolution) check(ctx context.Context, ref ports.ImageRef) error {
	pin, problems, err := checkImage(ctx, r.rc, ref, r.plan.revision)
	if errors.Is(err, ports.ErrNotFound) {
		r.missing(ctx, ref)
		return nil
	}
	if f, refused := ports.AsFail(err); refused {
		pin, problems, err = deploy.ImagePin{Tag: ref.Tag}, []string{f.Message}, nil
	}
	if err != nil {
		return err
	}
	r.record(ctx, ref, pin, problems)
	return nil
}

func (r *resolution) missing(ctx context.Context, ref ports.ImageRef) {
	if !r.plan.keepMissing {
		r.record(ctx, ref, deploy.ImagePin{Tag: ref.Tag}, []string{"tag not found"})
		return
	}
	r.rc.SetItem(ctx, deploy.Item{
		Key: string(ref.Image), Label: string(ref.Image), State: deploy.StateSkipped,
		Detail: "no " + string(ref.Tag) + " build, pin unchanged",
	})
}

func (r *resolution) record(ctx context.Context, ref ports.ImageRef, pin deploy.ImagePin, problems []string) {
	r.rc.SetItem(ctx, imageItem(ref, pin, problems))
	r.refused = appendProblems(r.refused, ref, problems)
	r.pins[ref.Image] = pin
}

func (r *resolution) refusal() error {
	if len(r.refused) == 0 {
		return nil
	}
	f := fail(deploy.FailDigestRefused, "%d of %d images refused", countImages(r.refused), len(r.plan.refs))
	f.LogTail = r.refused
	return f
}

func appendProblems(out []string, ref ports.ImageRef, problems []string) []string {
	for _, p := range problems {
		out = append(out, string(ref.Image)+":"+string(ref.Tag)+": "+p)
	}
	return out
}

func countImages(refused []string) int {
	seen := map[string]bool{}
	for _, r := range refused {
		img, _, _ := strings.Cut(r, ":")
		seen[img] = true
	}
	return len(seen)
}

func imageItem(ref ports.ImageRef, pin deploy.ImagePin, problems []string) deploy.Item {
	item := deploy.Item{Key: string(ref.Image), Label: string(ref.Image), State: deploy.StateSucceeded, Progress: deploy.Progress{Done: 1, Total: 1}}
	item.Detail = string(pin.Tag) + "@" + string(pin.Digest)
	if len(problems) > 0 {
		item.State, item.Detail = deploy.StateFailed, strings.Join(problems, "; ")
	}
	return item
}

func checkImage(ctx context.Context, rc *stage.RunCtx, ref ports.ImageRef, revision deploy.SHA) (deploy.ImagePin, []string, error) {
	info, err := rc.Deps.Registry.Resolve(ctx, ref)
	if err != nil {
		return deploy.ImagePin{}, nil, err
	}
	pin := deploy.ImagePin{Tag: ref.Tag, Digest: info.Digest}
	problems := indexProblems(info, revision)
	attested, err := rc.Deps.GitHub.AttestationExists(ctx, info.Digest)
	if err != nil {
		return pin, nil, err
	}
	if !attested {
		problems = append(problems, "no build provenance attestation")
	}
	return pin, problems, nil
}

func indexProblems(info ports.ImageInfo, revision deploy.SHA) []string {
	var problems []string
	for _, p := range requiredPlatforms {
		if !slices.Contains(info.Platforms, p) {
			problems = append(problems, "missing "+p)
		}
	}
	// An unset revision must not pass an unlabelled image.
	if revision == "" || info.Revision != revision {
		problems = append(problems, fmt.Sprintf("revision %q, want %q", info.Revision, revision))
	}
	return problems
}

func changedOnly(ctx context.Context, rc *stage.RunCtx, pins, current map[deploy.ImageName]deploy.ImagePin) map[deploy.ImageName]deploy.ImagePin {
	out := map[deploy.ImageName]deploy.ImagePin{}
	for img, pin := range pins {
		if current[img].Digest == pin.Digest {
			rc.SetItem(ctx, deploy.Item{Key: string(img), Label: string(img), State: deploy.StateSkipped, Detail: "digest unchanged, not pinned"})
			continue
		}
		out[img] = pin
	}
	return out
}

func mainPinLines(ctx context.Context, rc *stage.RunCtx) ([]PinLine, error) {
	_, files, err := mainManifests(ctx, rc)
	if err != nil {
		return nil, err
	}
	return ParsePins(files, rc.Deps.Config.ImageRepo), nil
}

func mainManifests(ctx context.Context, rc *stage.RunCtx) (deploy.SHA, ports.Files, error) {
	cfg := rc.Deps.Config
	head, err := rc.Deps.GitHub.BranchHead(ctx, cfg.MainBranch)
	if err != nil {
		return "", nil, err
	}
	files, err := manifestsAt(ctx, rc, ports.Ref(head))
	return head, files, err
}

func manifestsAt(ctx context.Context, rc *stage.RunCtx, ref ports.Ref) (ports.Files, error) {
	dir := rc.Deps.Config.ManifestDir
	tree, err := rc.Deps.GitHub.Tree(ctx, dir, ref)
	if err != nil {
		return nil, err
	}
	out := ports.Files{}
	for p, body := range tree {
		if path.Dir(string(p)) == string(dir) && path.Ext(string(p)) == ".yaml" {
			out[p] = body
		}
	}
	return out, nil
}

func sortedImages(pins map[deploy.ImageName]deploy.ImagePin) []deploy.ImageName {
	out := make([]deploy.ImageName, 0, len(pins))
	for img := range pins {
		out = append(out, img)
	}
	slices.Sort(out)
	return out
}
