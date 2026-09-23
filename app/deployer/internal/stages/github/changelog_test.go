// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// TestChangelogRenderMatchesHandWritten: an entry rendered by the deployer is
// byte-identical to the hand-written files it continues, so the marketing
// collection and future hand edits see no churn.
func TestChangelogRenderMatchesHandWritten(t *testing.T) {
	for _, name := range []string{"v0.2.1-beta.json", "v0.2.2-beta.json"} {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", "changelog", name))
			if err != nil {
				t.Fatal(err)
			}
			var src changelogFile
			if err := json.Unmarshal(want, &src); err != nil {
				t.Fatal(err)
			}
			entry := &deploy.ChangelogEntry{Title: src.Title, Highlights: src.Highlights, Date: src.Date}
			got, err := newChangelogFile(testConfig(), src.Version, entry).render()
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("render differs from %s:\n%s", name, got)
			}
		})
	}
}

func validEntry() *deploy.ChangelogEntry {
	return &deploy.ChangelogEntry{
		Title:      map[string]string{"en": "Deploys page", "fr": "Page des déploiements"},
		Highlights: map[string][]string{"en": {"Owners run the train & watch it."}, "fr": {"Les propriétaires lancent le train."}},
	}
}

func TestValidateChangelog(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*deploy.ChangelogEntry) *deploy.ChangelogEntry
		valid bool
	}{
		{"valid", func(e *deploy.ChangelogEntry) *deploy.ChangelogEntry { return e }, true},
		{"valid with date", func(e *deploy.ChangelogEntry) *deploy.ChangelogEntry { e.Date = "2026-09-23"; return e }, true},
		{"missing", func(*deploy.ChangelogEntry) *deploy.ChangelogEntry { return nil }, false},
		{"no English title", func(e *deploy.ChangelogEntry) *deploy.ChangelogEntry { delete(e.Title, "en"); return e }, false},
		{"no English highlights", func(e *deploy.ChangelogEntry) *deploy.ChangelogEntry { delete(e.Highlights, "en"); return e }, false},
		{"empty highlight", func(e *deploy.ChangelogEntry) *deploy.ChangelogEntry {
			e.Highlights["fr"] = append(e.Highlights["fr"], "")
			return e
		}, false},
		{"date not YYYY-MM-DD", func(e *deploy.ChangelogEntry) *deploy.ChangelogEntry { e.Date = "23/09/2026"; return e }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateChangelog(tc.edit(validEntry()))
			if valid := err == nil; valid != tc.valid {
				t.Errorf("err = %v, want valid %t", err, tc.valid)
			}
			if err != nil && !errors.Is(err, ports.ErrInvalid) {
				t.Errorf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

const changelogFilePath = "web/marketing/src/content/changelog/v0.2.3-beta.json"

type changelogResult struct {
	Done  bool
	Code  deploy.FailureCode
	Calls []string
	SHA   deploy.SHA
	PR    int
	Date  string
}

func TestChangelogStage(t *testing.T) {
	rendered := func() []byte {
		e := validEntry()
		e.Date = "2026-09-23"
		body, _ := newChangelogFile(testConfig(), "v0.2.3-beta", e).render()
		return body
	}()
	cases := []struct {
		name  string
		kind  deploy.RunKind
		entry *deploy.ChangelogEntry
		setup func(*fixture)
		want  changelogResult
	}{
		{
			name: "release commits, merges and pins the date", kind: deploy.KindRelease, entry: validEntry(),
			want: changelogResult{Calls: []string{
				"commit commit1 on chore/changelog-v0.2.3-beta from base: " + changelogFilePath,
				"pr #1002 chore/changelog-v0.2.3-beta: Add v0.2.3-beta release changelog",
				"merge #1002", "delete chore/changelog-v0.2.3-beta",
			}, SHA: "merge1002", PR: 1002, Date: "2026-09-23"},
		},
		{
			name: "exact entry already on main needs no PR", kind: deploy.KindRelease, entry: validEntry(),
			setup: func(f *fixture) { f.gh.commitMain("c1", ports.Files{changelogFilePath: rendered}) },
			want:  changelogResult{SHA: "c1", Date: "2026-09-23"},
		},
		{
			name: "merged changelog PR is done", kind: deploy.KindRelease, entry: validEntry(),
			setup: func(f *fixture) {
				pr := openPR(40, "h40")
				pr.HeadBranch, pr.Open, pr.Merged, pr.MergeSHA = "chore/changelog-v0.2.3-beta", false, true, "m40"
				f.gh.addPR(pr)
			},
			want: changelogResult{Done: true, SHA: "m40", PR: 40},
		},
		{
			name: "release without entry uses the file committed at the target", kind: deploy.KindRelease,
			setup: func(f *fixture) { f.gh.commitMain("c1", ports.Files{changelogFilePath: rendered}) },
			want:  changelogResult{},
		},
		{
			name: "release without entry or file is refused", kind: deploy.KindRelease,
			want: changelogResult{Code: "invalid"},
		},
		{
			name: "hotfix without entry keeps the released text", kind: deploy.KindHotfix,
			want: changelogResult{Code: "skipped"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newRun(tc.kind)
			run.Version, run.Changelog = "v0.2.3-beta", tc.entry
			f := newFixture(t, run)
			if tc.setup != nil {
				tc.setup(f)
			}
			f.sink.run.TargetSHA = f.gh.head()
			done, err := f.runStage(t, deploy.StageChangelog)
			got := f.sink.View()
			res := changelogResult{Done: done, Code: outcome(t, err), Calls: f.gh.calls, SHA: got.Outputs.ChangelogSHA, PR: got.Outputs.ChangelogPR}
			if got.Changelog != nil {
				res.Date = got.Changelog.Date
			}
			if !reflect.DeepEqual(res, tc.want) {
				t.Errorf("result = %+v, want %+v", res, tc.want)
			}
		})
	}
}

// TestChangelogStageWritesRenderedFile: what lands on main is the rendered
// entry with the pinned date and the release link.
func TestChangelogStageWritesRenderedFile(t *testing.T) {
	run := newRun(deploy.KindRelease)
	run.Version, run.Changelog = "v0.2.3-beta", validEntry()
	f := newFixture(t, run)
	if _, err := f.runStage(t, deploy.StageChangelog); err != nil {
		t.Fatal(err)
	}
	var got changelogFile
	if err := json.Unmarshal(f.gh.trees[f.gh.head()][changelogFilePath], &got); err != nil {
		t.Fatal(err)
	}
	want := changelogFile{
		Tag: "beta", Version: "v0.2.3-beta", Date: "2026-09-23", Title: validEntry().Title, Highlights: validEntry().Highlights,
		GitHub: "https://github.com/AdamOusmer/ItsBagelBot/releases/tag/v0.2.3-beta",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("file = %+v, want %+v", got, want)
	}
}
