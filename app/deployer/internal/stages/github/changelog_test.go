// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/codec"
)

func TestChangelogStageWritesTheHandWrittenFiles(t *testing.T) {
	for _, name := range []string{"v0.2.1-beta.json", "v0.2.2-beta.json"} {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", "changelog", name))
			require.NoError(t, err)
			var src changelogFile
			require.NoError(t, codec.Unmarshal(want, &src))
			run := newRun(deploy.KindRelease)
			run.Version, run.Changelog = src.Version, &deploy.ChangelogEntry{Title: src.Title, Highlights: src.Highlights, Date: src.Date}
			f := newFixture(t, run)

			_, err = f.runStage(t, deploy.StageChangelog)

			require.NoError(t, err)
			assert.Equal(t, string(want), string(f.gh.trees[f.gh.head()]["web/marketing/src/content/changelog/"+ports.FilePath(name)]))
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
			if tc.valid {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, ports.ErrInvalid)
		})
	}
}

const (
	changelogFilePath  = "web/marketing/src/content/changelog/v0.2.3-beta.json"
	committedChangelog = `{"tag":"beta","version":"v0.2.3-beta","date":"2026-09-23",
		"title":{"en":"Deploys page","fr":"Page des déploiements"},
		"highlights":{"en":["Owners run the train & watch it."],"fr":["Les propriétaires lancent le train."]},
		"github":"https://github.com/AdamOusmer/ItsBagelBot/releases/tag/v0.2.3-beta"}`
)

type changelogResult struct {
	Done  bool
	Code  deploy.FailureCode
	Calls []string
	SHA   deploy.SHA
	PR    int
	Date  string
}

func TestChangelogStage(t *testing.T) {
	rendered := []byte(committedChangelog)
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
			assert.Equal(t, tc.want, res)
		})
	}
}

func TestChangelogStageWritesRenderedFile(t *testing.T) {
	run := newRun(deploy.KindRelease)
	run.Version, run.Changelog = "v0.2.3-beta", validEntry()
	f := newFixture(t, run)

	_, err := f.runStage(t, deploy.StageChangelog)

	require.NoError(t, err)
	var got changelogFile
	require.NoError(t, codec.Unmarshal(f.gh.trees[f.gh.head()][changelogFilePath], &got))
	assert.Equal(t, changelogFile{
		Tag: "beta", Version: "v0.2.3-beta", Date: "2026-09-23", Title: validEntry().Title, Highlights: validEntry().Highlights,
		GitHub: "https://github.com/AdamOusmer/ItsBagelBot/releases/tag/v0.2.3-beta",
	}, got)
}
