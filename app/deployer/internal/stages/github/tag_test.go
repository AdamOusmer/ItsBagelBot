// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"reflect"
	"testing"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

type tagResult struct {
	Done  bool
	Code  deploy.FailureCode
	Calls []string
	TagAt deploy.SHA
}

// Main is base, c1 (changelog merge), c2 in every case; pr-head is a
// commit main does not contain.
func TestTagStage(t *testing.T) {
	cases := []struct {
		name      string
		kind      deploy.RunKind
		changelog deploy.SHA
		target    deploy.SHA
		existing  deploy.SHA
		want      tagResult
	}{
		{"release tags the changelog merge", deploy.KindRelease, "c1", "c2", "",
			tagResult{Calls: []string{"tag v0.2.3-beta -> c1 force=false"}, TagAt: "c1"}},
		{"release without changelog commit tags the target", deploy.KindRelease, "", "c2", "",
			tagResult{Calls: []string{"tag v0.2.3-beta -> c2 force=false"}, TagAt: "c2"}},
		{"tag already at the commit is done", deploy.KindRelease, "c1", "c2", "c1",
			tagResult{Done: true, TagAt: "c1"}},
		{"release never moves a tag", deploy.KindRelease, "c1", "c2", "base",
			tagResult{Code: deploy.FailGitHub}},
		{"hotfix force-moves the tag forward", deploy.KindHotfix, "", "c2", "c1",
			tagResult{Calls: []string{"tag v0.2.3-beta -> c2 force=true"}, TagAt: "c2"}},
		{"hotfix never moves the tag backwards", deploy.KindHotfix, "", "c1", "c2",
			tagResult{Code: deploy.FailGitHub}},
		{"release refuses a commit off main", deploy.KindRelease, "", "pr-head", "",
			tagResult{Code: deploy.FailGitHub}},
		{"hotfix refuses a commit off main", deploy.KindHotfix, "", "pr-head", "c1",
			tagResult{Code: deploy.FailGitHub}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newRun(tc.kind)
			run.Version, run.TargetSHA, run.Outputs.ChangelogSHA = "v0.2.3-beta", tc.target, tc.changelog
			f := newFixture(t, run)
			f.gh.commitMain("c1", nil)
			f.gh.commitMain("c2", nil)
			f.gh.offMain["pr-head"] = true
			if tc.existing != "" {
				f.gh.tags["v0.2.3-beta"] = ports.TagRef{Name: "v0.2.3-beta", CommitSHA: tc.existing}
			}
			done, err := f.runStage(t, deploy.StageTag)
			res := tagResult{Done: done, Code: outcome(t, err), Calls: f.gh.calls, TagAt: f.sink.View().Outputs.TagSHA}
			if !reflect.DeepEqual(res, tc.want) {
				t.Errorf("result = %+v, want %+v", res, tc.want)
			}
		})
	}
}

type releaseResult struct {
	Done  bool
	Code  deploy.FailureCode
	Calls []string
	Notes string
	URL   string
}

func TestReleaseStage(t *testing.T) {
	entry := validEntry()
	entry.Highlights["en"] = []string{"First.", "Second <b> & more."}
	entry.Date = "2026-09-23"
	body, err := newChangelogFile(testConfig(), "v0.2.3-beta", entry).render()
	if err != nil {
		t.Fatal(err)
	}
	const url = "https://github.test/releases/tag/v0.2.3-beta"
	cases := []struct {
		name     string
		existing *ports.Release
		want     releaseResult
	}{
		{"creates the release from the changelog at the tag", nil, releaseResult{
			Calls: []string{"release v0.2.3-beta at c1"}, URL: url,
			Notes: "## Highlights\n\n- First.\n\n- Second <b> & more.\n",
		}},
		{"latest release at the tag is done", &ports.Release{Tag: "v0.2.3-beta", URL: url, TargetCommitish: "c1", Latest: true},
			releaseResult{Done: true, URL: url}},
		{"release at the old tag commit is re-pointed", &ports.Release{Tag: "v0.2.3-beta", URL: url, TargetCommitish: "base", Latest: true},
			releaseResult{Calls: []string{"release v0.2.3-beta at c1"}, URL: url, Notes: "## Highlights\n\n- First.\n\n- Second <b> & more.\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newRun(deploy.KindHotfix)
			run.Version, run.Outputs.TagSHA = "v0.2.3-beta", "c1"
			f := newFixture(t, run)
			f.gh.commitMain("c1", ports.Files{changelogFilePath: body})
			if tc.existing != nil {
				f.gh.releases["v0.2.3-beta"] = *tc.existing
			}
			done, err := f.runStage(t, deploy.StageRelease)
			res := releaseResult{Done: done, Code: outcome(t, err), Calls: f.gh.calls, Notes: f.gh.notes["v0.2.3-beta"], URL: f.sink.View().Outputs.ReleaseURL}
			if !reflect.DeepEqual(res, tc.want) {
				t.Errorf("result = %+v, want %+v", res, tc.want)
			}
		})
	}
}
