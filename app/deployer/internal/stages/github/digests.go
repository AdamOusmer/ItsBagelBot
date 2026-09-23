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

// digests resolves every image the run will pin and refuses any that is not
// provably the build of the target commit: both architectures in the index
// (the cluster runs ARM and Intel nodes, a single-arch index schedules and
// then fails to pull on the other half), the revision label equal to the
// commit, and a build provenance attestation for the digest.

var requiredPlatforms = []string{"linux/amd64", "linux/arm64"}

func digestsDone(_ context.Context, rc *stage.RunCtx) (bool, error) {
	return len(rc.View().Outputs.Digests) > 0, nil
}

// digestPlan is what runDigests resolves: tag per image and the commit every
// image must have been built from.
type digestPlan struct {
	refs     []ports.ImageRef
	revision deploy.SHA
	// current are the pins on main, so a bump can drop images whose digest
	// did not change.
	current map[deploy.ImageName]deploy.ImagePin
	// keepMissing leaves an image without the tag at its current pin instead
	// of refusing it. Only a rollback sets it: an image added after the
	// target release (the deployer itself, a new service) has no build of
	// that version, and refusing it would make every older release
	// unreachable.
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

// versionRefs is every image main pins at the version tag, the tag a tag push
// gives every image (publish-images builds them all for a v* ref).
func versionRefs(current map[deploy.ImageName]deploy.ImagePin, v deploy.Version) []ports.ImageRef {
	refs := make([]ports.ImageRef, 0, len(current))
	for _, img := range sortedImages(current) {
		refs = append(refs, ports.ImageRef{Image: img, Tag: deploy.Tag(v)})
	}
	return refs
}

// bumpRefs: the images the main push run built (and the operator did not
// narrow away), each at the newest main-<ts>-<sha12> tag of the target commit.
// The timestamp is the run's, unknown until the tags exist, hence the lookup;
// a rerun of the same commit adds a newer one.
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

// bumpImages keeps the built images that main pins and, when the operator
// narrowed the bump, that a named service runs.
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

// resolveAll checks every image and refuses the lot if any fails: a
// partial pin set would roll some services to the new build and leave the
// rest behind with no record of which.
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

// resolution collects the pins and every problem found, so one refusal lists
// all the bad images instead of the first.
type resolution struct {
	rc      *stage.RunCtx
	plan    digestPlan
	pins    map[deploy.ImageName]deploy.ImagePin
	refused []string
}

// check sorts the three ways an image can answer: a tag that does not exist,
// an index the registry adapter refused itself (single-platform, missing an
// arch; a *Fail recorded as a problem so the page still lists the rest), and
// the registry or GitHub failing to answer at all, which stops the stage.
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

// checkImage returns the pin and what is wrong with it; err is the registry's
// own answer (not found, refused) or a failure to answer.
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
	// An empty want never matches: an unset TagSHA must not pass an image
	// that carries no label either.
	if revision == "" || info.Revision != revision {
		problems = append(problems, fmt.Sprintf("revision %q, want %q", info.Revision, revision))
	}
	return problems
}

// changedOnly drops the images whose digest main already pins. A bump that
// only changed the tag string would still change the pod template and
// restart the service for nothing.
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

// mainPinLines reads the pins on main.
func mainPinLines(ctx context.Context, rc *stage.RunCtx) ([]PinLine, error) {
	_, files, err := mainManifests(ctx, rc)
	if err != nil {
		return nil, err
	}
	return ParsePins(files, rc.Deps.Config.ImageRepo), nil
}

// mainManifests reads deploy/k8s/*.yaml at main's head. Only the top level:
// the bench and network-tune subdirectories are not applied by the train.
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
