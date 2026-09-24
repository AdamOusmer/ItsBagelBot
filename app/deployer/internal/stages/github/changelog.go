// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/codec"
)

type changelogFile struct {
	Tag        string              `json:"tag"`
	Version    deploy.Version      `json:"version"`
	Date       string              `json:"date"`
	Title      map[string]string   `json:"title"`
	Highlights map[string][]string `json:"highlights"`
	GitHub     string              `json:"github"`
}

const dateLayout = "2006-01-02"

func ValidateChangelog(e *deploy.ChangelogEntry) error {
	switch {
	case e == nil:
		return fmt.Errorf("%w: changelog missing", ports.ErrInvalid)
	case e.Title["en"] == "":
		return fmt.Errorf("%w: changelog needs an English title", ports.ErrInvalid)
	case len(e.Highlights["en"]) == 0:
		return fmt.Errorf("%w: changelog needs English highlights", ports.ErrInvalid)
	}
	if err := noEmptyHighlight(e.Highlights); err != nil {
		return err
	}
	if _, err := time.Parse(dateLayout, e.Date); e.Date != "" && err != nil {
		return fmt.Errorf("%w: changelog date %q is not YYYY-MM-DD", ports.ErrInvalid, e.Date)
	}
	return nil
}

func noEmptyHighlight(h map[string][]string) error {
	for locale, lines := range h {
		for _, line := range lines {
			if line == "" {
				return fmt.Errorf("%w: changelog has an empty %s highlight", ports.ErrInvalid, locale)
			}
		}
	}
	return nil
}

func newChangelogFile(cfg ports.Config, v deploy.Version, e *deploy.ChangelogEntry) changelogFile {
	return changelogFile{
		Tag: "beta", Version: v, Date: e.Date, Title: e.Title, Highlights: e.Highlights,
		GitHub: releaseURL(cfg, v),
	}
}

func (f changelogFile) render() ([]byte, error) {
	var buf bytes.Buffer
	enc := codec.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func changelogDone(ctx context.Context, rc *stage.RunCtx) (bool, error) {
	run := rc.View()
	if run.Changelog == nil {
		return false, nil
	}
	pr, found, err := rc.Deps.GitHub.FindPR(ctx, changelogBranch(run))
	if err != nil || !found {
		return false, err
	}
	if !pr.Merged {
		return false, nil
	}
	return true, recordChangelog(ctx, rc, pr)
}

func recordChangelog(ctx context.Context, rc *stage.RunCtx, pr ports.PullRequest) error {
	rc.AddLink(ctx, deploy.Link{Label: "Changelog PR", URL: pr.URL})
	return rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.ChangelogPR, o.ChangelogSHA = pr.Number, pr.MergeSHA })
}

func runChangelog(ctx context.Context, rc *stage.RunCtx) error {
	run := rc.View()
	if run.Changelog == nil {
		return noChangelog(ctx, rc, run)
	}
	if err := ValidateChangelog(run.Changelog); err != nil {
		return err
	}
	cfg := rc.Deps.Config
	onMain, err := readChangelog(ctx, rc.Deps.GitHub, changelogPath(cfg, run.Version), mainRef(cfg))
	if err != nil {
		return err
	}
	entry, err := pinDate(ctx, rc, onMain)
	if err != nil {
		return err
	}
	want := newChangelogFile(cfg, run.Version, entry)
	if onMain != nil && reflect.DeepEqual(*onMain, want) {
		return changelogOnMain(ctx, rc)
	}
	return commitChangelog(ctx, rc, want)
}

func noChangelog(ctx context.Context, rc *stage.RunCtx, run deploy.Run) error {
	if run.Kind == deploy.KindHotfix {
		return stage.ErrSkipped
	}
	p := changelogPath(rc.Deps.Config, run.Version)
	_, err := rc.Deps.GitHub.File(ctx, p, ports.Ref(run.TargetSHA))
	if errors.Is(err, ports.ErrNotFound) {
		return fmt.Errorf("%w: no changelog in the request and none at %s", ports.ErrInvalid, p)
	}
	return err
}

func readChangelog(ctx context.Context, gh ports.GitHub, p ports.FilePath, ref ports.Ref) (*changelogFile, error) {
	body, err := gh.File(ctx, p, ref)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f changelogFile
	if err := codec.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("%s at %s: %w", p, ref, err)
	}
	return &f, nil
}

func pinDate(ctx context.Context, rc *stage.RunCtx, onMain *changelogFile) (*deploy.ChangelogEntry, error) {
	entry := *rc.View().Changelog
	if entry.Date != "" {
		return &entry, nil
	}
	entry.Date = rc.Deps.Clock.Now().UTC().Format(dateLayout)
	if onMain != nil && rc.View().Kind == deploy.KindHotfix {
		entry.Date = onMain.Date
	}
	err := rc.Update(ctx, func(r *deploy.Run) { r.Changelog = &entry })
	return &entry, err
}

func changelogOnMain(ctx context.Context, rc *stage.RunCtx) error {
	head, err := rc.Deps.GitHub.BranchHead(ctx, rc.Deps.Config.MainBranch)
	if err != nil {
		return err
	}
	return rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.ChangelogSHA = head })
}

func commitChangelog(ctx context.Context, rc *stage.RunCtx, want changelogFile) error {
	body, err := want.render()
	if err != nil {
		return err
	}
	run := rc.View()
	title := "Add " + string(run.Version) + " release changelog"
	if run.Kind == deploy.KindHotfix {
		title = "Refresh " + string(run.Version) + " changelog"
	}
	pr, err := ensurePR(ctx, rc, prRequest{
		Branch: changelogBranch(run), Title: title,
		Body:  "Changelog for " + string(run.Version) + ", opened by the deployer.",
		Files: ports.Files{changelogPath(rc.Deps.Config, run.Version): body},
	})
	if err != nil {
		return err
	}
	merged, err := mergePR(ctx, rc, pr.Number)
	if err != nil {
		return err
	}
	return recordChangelog(ctx, rc, merged)
}

type prRequest struct {
	Branch ports.Branch
	// Must be the commit the files were read at: a newer base silently reverts what landed between.
	Base  deploy.SHA
	Title string
	Body  string
	Files ports.Files
}

func ensurePR(ctx context.Context, rc *stage.RunCtx, req prRequest) (ports.PullRequest, error) {
	gh := rc.Deps.GitHub
	pr, found, err := gh.FindPR(ctx, req.Branch)
	if err != nil || found {
		return pr, err
	}
	base, err := prBase(ctx, rc, req)
	if err != nil {
		return ports.PullRequest{}, err
	}
	spec := ports.CommitSpec{Branch: req.Branch, Base: base, Message: req.Title, Files: req.Files}
	if err := commitBranch(ctx, gh, spec); err != nil {
		return ports.PullRequest{}, err
	}
	pr, err = gh.CreatePR(ctx, ports.PRSpec{Head: req.Branch, Base: rc.Deps.Config.MainBranch, Title: req.Title, Body: req.Body})
	if err == nil {
		rc.AddLink(ctx, deploy.Link{Label: "PR #" + fmt.Sprint(pr.Number), URL: pr.URL})
	}
	return pr, err
}

func prBase(ctx context.Context, rc *stage.RunCtx, req prRequest) (deploy.SHA, error) {
	if req.Base != "" {
		return req.Base, nil
	}
	return rc.Deps.GitHub.BranchHead(ctx, rc.Deps.Config.MainBranch)
}

func commitBranch(ctx context.Context, gh ports.GitHub, spec ports.CommitSpec) error {
	_, err := gh.CommitFiles(ctx, spec)
	if !errors.Is(err, ports.ErrConflict) {
		return err
	}
	if err := gh.DeleteBranch(ctx, spec.Branch); err != nil {
		return err
	}
	_, err = gh.CommitFiles(ctx, spec)
	return err
}
