// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"context"
	"fmt"
	"strings"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// pin_pr rewrites main's pin lines to the run's digests in one PR and merges
// it. Rollout reads the manifests at that merge commit, never the working
// tree or a later main, so what rolls is exactly what the PR showed.
//
// A rollback has no digests stage: its pins are the version tag of the target
// release, resolved and checked here the way digests checks a release. The
// tag and not the old pin commit, because a hotfix force-moves the tag and
// its pin PR carries a run key no lookup can find again: the release's own
// pin commit would roll back to the build the hotfix replaced.

func pinsDone(ctx context.Context, rc *stage.RunCtx) (bool, error) {
	run := rc.View()
	if len(run.Outputs.Digests) == 0 {
		// Nothing resolved yet (a rollback resolves in Run).
		return false, nil
	}
	plan, err := planPins(ctx, rc, run)
	if err != nil || len(plan.changed) > 0 {
		return false, err
	}
	return true, recordPins(ctx, rc, plan, ports.PullRequest{MergeSHA: plan.head})
}

func runPins(ctx context.Context, rc *stage.RunCtx) error {
	run := rc.View()
	plan, err := planPins(ctx, rc, run)
	if err != nil {
		return err
	}
	plan.report(ctx, rc, deploy.StatePending)
	if len(plan.changed) == 0 {
		return recordPins(ctx, rc, plan, ports.PullRequest{MergeSHA: plan.head})
	}
	pr, err := ensurePR(ctx, rc, plan.request(run))
	if err != nil {
		return err
	}
	merged, err := mergePR(ctx, rc, pr.Number)
	if err != nil {
		return err
	}
	return recordPins(ctx, rc, plan, merged)
}

// pinPlan is main's pins against the run's.
type pinPlan struct {
	head     deploy.SHA // main head the manifests were read at, the PR base
	lines    []PinLine
	pins     map[deploy.ImageName]deploy.ImagePin
	changed  ports.Files
	services []string
}

func planPins(ctx context.Context, rc *stage.RunCtx, run deploy.Run) (pinPlan, error) {
	head, files, err := mainManifests(ctx, rc)
	if err != nil {
		return pinPlan{}, err
	}
	repo := rc.Deps.Config.ImageRepo
	plan := pinPlan{head: head, lines: ParsePins(files, repo)}
	if plan.pins, err = targetPins(ctx, rc, run, plan.lines); err != nil {
		return plan, err
	}
	if plan.changed, err = RewritePins(files, repo, plan.pins); err != nil {
		return plan, err
	}
	plan.services = pinServices(run.Kind, plan.lines, plan.pins)
	return plan, nil
}

func targetPins(ctx context.Context, rc *stage.RunCtx, run deploy.Run, lines []PinLine) (map[deploy.ImageName]deploy.ImagePin, error) {
	switch {
	case len(run.Outputs.Digests) > 0:
		return run.Outputs.Digests, nil
	case run.Kind == deploy.KindRollback:
		return rollbackPins(ctx, rc, run, lines)
	}
	return nil, fmt.Errorf("%w: no digests to pin", ports.ErrInvalid)
}

// rollbackPins resolves the target release's version tag for every image
// main pins and persists them as the run's digests, so a resumed attempt and
// verify see the same set.
func rollbackPins(ctx context.Context, rc *stage.RunCtx, run deploy.Run, lines []PinLine) (map[deploy.ImageName]deploy.ImagePin, error) {
	ref, found, err := rc.Deps.GitHub.Tag(ctx, run.RollbackTo)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: no tag %q to roll back to", ports.ErrInvalid, run.RollbackTo)
	}
	pins, err := resolveAll(ctx, rc, digestPlan{
		refs: versionRefs(pinsByImage(lines), run.RollbackTo), revision: ref.CommitSHA, keepMissing: true,
	})
	if err != nil {
		return nil, err
	}
	if len(pins) == 0 {
		return nil, fail(deploy.FailDigestRefused, "no image has a %s build", run.RollbackTo)
	}
	return pins, rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.Digests = pins })
}

// pinServices follows Outputs.Services: a bump lists the rollout units whose
// image it pins, every other kind lists them all.
func pinServices(kind deploy.RunKind, lines []PinLine, pins map[deploy.ImageName]deploy.ImagePin) []string {
	if kind == deploy.KindBump {
		return servicesFor(lines, pins)
	}
	return servicesFor(lines, pinsByImage(lines))
}

// recordPins stores the commit rollout reads: the pin PR's merge commit, or
// main's head when main already carried every pin.
func recordPins(ctx context.Context, rc *stage.RunCtx, plan pinPlan, pr ports.PullRequest) error {
	plan.report(ctx, rc, deploy.StateSucceeded)
	rc.SetProgress(ctx, deploy.Progress{Done: len(plan.pins), Total: len(plan.pins)})
	if pr.URL != "" {
		rc.AddLink(ctx, deploy.Link{Label: "Pin PR #" + fmt.Sprint(pr.Number), URL: pr.URL})
	}
	return rc.SetOutputs(ctx, func(o *deploy.Outputs) {
		o.Services, o.PinPR, o.PinSHA = plan.services, pr.Number, pr.MergeSHA
	})
}

// report upserts one row per pinned image, "old -> new" or skipped when
// main already has it. Upsert rather than replace: the PR row mergePR adds
// and a rollback's "no build" rows from the resolve stay on the page.
func (p pinPlan) report(ctx context.Context, rc *stage.RunCtx, state deploy.StageState) {
	current := pinsByImage(p.lines)
	for _, img := range sortedImages(p.pins) {
		rc.SetItem(ctx, pinItem(img, current[img], p.pins[img], state))
	}
}

func pinItem(img deploy.ImageName, from, to deploy.ImagePin, state deploy.StageState) deploy.Item {
	item := deploy.Item{Key: string(img), Label: string(img), State: state, Detail: pinChange(from, to)}
	if from == to {
		item.State, item.Detail = deploy.StateSkipped, "already pinned "+string(to.Tag)
	}
	return item
}

func pinChange(from, to deploy.ImagePin) string {
	return string(from.Tag) + " -> " + string(to.Tag) + "@" + string(to.Digest)
}

func (p pinPlan) request(run deploy.Run) prRequest {
	return prRequest{
		Branch: pinBranch(run), Base: p.head, Title: pinTitle(run, p.services),
		Body: p.body(run), Files: p.changed,
	}
}

// pinTitle follows the titles the hand-made pin PRs used ("Deploy
// v0.2.2-beta release images", #994) so the history reads the same after the
// deployer took over.
func pinTitle(run deploy.Run, services []string) string {
	switch run.Kind {
	case deploy.KindBump:
		return "chore(deploy): bump " + joinList(services)
	case deploy.KindRollback:
		return "Rollback to " + string(run.RollbackTo)
	case deploy.KindHotfix:
		return "Deploy " + string(run.Version) + " hotfix images"
	}
	return "Deploy " + string(run.Version) + " release images"
}

func (p pinPlan) body(run deploy.Run) string {
	var b strings.Builder
	b.WriteString("Opened by the deployer for run " + string(run.ID) + ".\n\n")
	current := pinsByImage(p.lines)
	for _, img := range sortedImages(p.pins) {
		b.WriteString("- `" + string(img) + "`: " + pinChange(current[img], p.pins[img]) + "\n")
	}
	return b.String()
}
