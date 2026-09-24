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

func pinsDone(ctx context.Context, rc *stage.RunCtx) (bool, error) {
	run := rc.View()
	if len(run.Outputs.Digests) == 0 {
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

type pinPlan struct {
	head     deploy.SHA
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

func pinServices(kind deploy.RunKind, lines []PinLine, pins map[deploy.ImageName]deploy.ImagePin) []string {
	if kind == deploy.KindBump {
		return servicesFor(lines, pins)
	}
	return servicesFor(lines, pinsByImage(lines))
}

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
