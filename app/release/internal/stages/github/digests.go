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

	"ItsBagelBot/app/release/internal/ports"
	"ItsBagelBot/app/release/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// Workload nodes are Intel only; cp1 is ARM but tainted and excluded by every workload's affinity.
var requiredPlatforms = []string{"linux/amd64"}

func digestsDone(_ context.Context, rc *stage.RunCtx) (bool, error) {
	return len(rc.View().Outputs.Digests) > 0, nil
}

type digestPlan struct {
	refs        []ports.ImageRef
	revision    deploy.SHA
	built       map[deploy.ImageName]deploy.SHA
	current     map[deploy.ImageName]deploy.ImagePin
	keepMissing bool
}

func (p digestPlan) revisionOf(img deploy.ImageName) deploy.SHA {
	if sha, ok := p.built[img]; ok {
		return sha
	}
	return p.revision
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
		if err := refuseDowngrades(ctx, rc, pins, plan); err != nil {
			return err
		}
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
	refs, built, err := bumpRefs(ctx, rc, run, lines)
	return digestPlan{refs: refs, revision: run.TargetSHA, built: built, current: current}, err
}

func versionRefs(current map[deploy.ImageName]deploy.ImagePin, v deploy.Version) []ports.ImageRef {
	refs := make([]ports.ImageRef, 0, len(current))
	for _, img := range sortedImages(current) {
		refs = append(refs, ports.ImageRef{Image: img, Tag: deploy.Tag(v)})
	}
	return refs
}

func bumpRefs(ctx context.Context, rc *stage.RunCtx, run deploy.Run, lines []PinLine) ([]ports.ImageRef, map[deploy.ImageName]deploy.SHA, error) {
	jobs, err := rc.Deps.GitHub.RunJobs(ctx, run.Outputs.BuildRunID)
	if err != nil {
		return nil, nil, err
	}
	built := builtImages(jobs)
	var refs []ports.ImageRef
	for _, img := range bumpImages(built, lines, run.Services) {
		tag, err := mainTag(ctx, rc.Deps.Registry, img, run.TargetSHA)
		if err != nil {
			return nil, nil, err
		}
		refs = append(refs, ports.ImageRef{Image: img, Tag: tag})
	}
	latest, revisions, err := latestBuilds(ctx, rc, unbuiltRequested(built, lines, run.Services), run.TargetSHA)
	return append(refs, latest...), revisions, err
}

// A requested service the target commit did not rebuild ships its newest main build at or before the target.
func unbuiltRequested(built []deploy.ImageName, lines []PinLine, services []string) []deploy.ImageName {
	var out []deploy.ImageName
	for _, l := range lines {
		if slices.Contains(services, l.Workload) && !alreadyListed(l.Image, built, out) {
			out = append(out, l.Image)
		}
	}
	return out
}

func alreadyListed(img deploy.ImageName, built, out []deploy.ImageName) bool {
	return slices.Contains(built, img) || slices.Contains(out, img)
}

func latestBuilds(ctx context.Context, rc *stage.RunCtx, images []deploy.ImageName, target deploy.SHA) ([]ports.ImageRef, map[deploy.ImageName]deploy.SHA, error) {
	refs := make([]ports.ImageRef, 0, len(images))
	revisions := map[deploy.ImageName]deploy.SHA{}
	for _, img := range images {
		tag, sha, err := latestMainTag(ctx, rc, img, target)
		if err != nil {
			return nil, nil, err
		}
		refs = append(refs, ports.ImageRef{Image: img, Tag: tag})
		revisions[img] = sha
	}
	return refs, revisions, nil
}

func latestMainTag(ctx context.Context, rc *stage.RunCtx, img deploy.ImageName, target deploy.SHA) (deploy.Tag, deploy.SHA, error) {
	tags, err := rc.Deps.Registry.Tags(ctx, img)
	if err != nil {
		return "", "", err
	}
	for _, t := range newestMainTags(tags) {
		sha := deploy.SHA(mainTagCommit.FindStringSubmatch(string(t))[1])
		cmp, err := rc.Deps.GitHub.Compare(ctx, ports.Ref(sha), ports.Ref(target))
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		if cmp.BehindBy == 0 {
			return t, sha, nil
		}
	}
	return "", "", fail(deploy.FailDigestRefused, "%s has no main build at or before %s", img, short(target))
}

func newestMainTags(tags []deploy.Tag) []deploy.Tag {
	var out []deploy.Tag
	for _, t := range tags {
		if mainTagCommit.MatchString(string(t)) {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b deploy.Tag) int { return compareTagTime(b, a) })
	return out
}

func compareTagTime(a, b deploy.Tag) int {
	return int(tagTime(a) - tagTime(b))
}

func tagTime(t deploy.Tag) int64 {
	ts, _ := strconv.ParseInt(strings.SplitN(string(t), "-", 3)[1], 10, 64)
	return ts
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
	pin, problems, err := checkImage(ctx, r.rc, ref, r.plan.revisionOf(ref.Image))
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
	if !builtFrom(info.Revision, revision) {
		problems = append(problems, fmt.Sprintf("revision %q, want %q", info.Revision, revision))
	}
	return problems
}

// An unset revision must not pass an unlabelled image; a 12-character revision comes from a main-<ts>-<sha> tag.
func builtFrom(label, revision deploy.SHA) bool {
	if revision == "" {
		return false
	}
	short := len(revision) == 12 && strings.HasPrefix(string(label), string(revision))
	return label == revision || short
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

var mainTagCommit = regexp.MustCompile(`^main-\d+-([0-9a-f]+)$`)

// A bump to a commit older than a live pin silently reverts merged work; that is a rollback, not a bump.
func refuseDowngrades(ctx context.Context, rc *stage.RunCtx, pins map[deploy.ImageName]deploy.ImagePin, plan digestPlan) error {
	var refused []string
	for _, img := range sortedImages(pins) {
		older, err := olderThanPin(ctx, rc, plan.current[img], plan.revisionOf(img))
		if err != nil {
			return err
		}
		if older {
			refused = append(refused, fmt.Sprintf("%s: %s is older than the pinned %s", img, short(plan.revisionOf(img)), plan.current[img].Tag))
		}
	}
	if len(refused) == 0 {
		return nil
	}
	f := fail(deploy.FailDigestRefused, "bump would move %d image(s) to an older build; ship a newer commit or run a rollback", len(refused))
	f.LogTail = refused
	return f
}

func olderThanPin(ctx context.Context, rc *stage.RunCtx, pin deploy.ImagePin, target deploy.SHA) (bool, error) {
	m := mainTagCommit.FindStringSubmatch(string(pin.Tag))
	if m == nil {
		return false, nil
	}
	cmp, err := rc.Deps.GitHub.Compare(ctx, ports.Ref(m[1]), ports.Ref(target))
	if errors.Is(err, ports.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return cmp.BehindBy > 0 && cmp.AheadBy == 0, nil
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
