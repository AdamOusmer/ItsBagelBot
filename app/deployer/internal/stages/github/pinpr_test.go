// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"reflect"
	"slices"
	"testing"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

var allServices = []string{
	"commands", "console-admin", "console-dashboard", "discord-data", "discord-ingress", "discord-engine",
	"discord-outgress", "gossip", "loyalty", "modules", "notifications", "outgress", "projector", "sesame",
	"transactions", "twitch-ingress", "users",
}

func pinsAt(tag deploy.Tag, images ...deploy.ImageName) map[deploy.ImageName]deploy.ImagePin {
	out := map[deploy.ImageName]deploy.ImagePin{}
	for _, img := range images {
		out[img] = deploy.ImagePin{Tag: tag, Digest: testDigest(string(img) + string(tag))}
	}
	return out
}

type pinResult struct {
	Done     bool
	Code     deploy.FailureCode
	Title    string
	Branch   ports.Branch
	Files    int
	PinPR    int
	PinSHA   deploy.SHA
	Services []string
	OnMain   bool
}

func (f *fixture) pinResult(t *testing.T, done bool, err error) pinResult {
	t.Helper()
	run := f.sink.View()
	res := pinResult{
		Done: done, Code: outcome(t, err), PinPR: run.Outputs.PinPR, PinSHA: run.Outputs.PinSHA,
		Services: run.Outputs.Services, OnMain: f.mainHas(run.Outputs.Digests),
	}
	if pr, ok := f.gh.prs[run.Outputs.PinPR]; ok {
		res.Title, res.Branch, res.Files = pr.Title, pr.HeadBranch, len(pr.files)
	}
	return res
}

func (f *fixture) mainHas(pins map[deploy.ImageName]deploy.ImagePin) bool {
	onMain := pinsByImage(ParsePins(f.gh.trees[f.gh.head()], testRepo))
	for img, pin := range pins {
		if onMain[img] != pin {
			return false
		}
	}
	return len(pins) > 0
}

func TestPinPRStage(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T, *deploy.Run)
		want  pinResult
	}{
		{
			name:  "release pins through a merged PR",
			setup: func(_ *testing.T, r *deploy.Run) { r.Outputs.Digests = pinsAt("v0.2.3-beta", "users", "gossip") },
			want: pinResult{
				Title: "Deploy v0.2.3-beta release images", Branch: "chore/deploy-v0.2.3-beta", Files: 2,
				PinPR: 1002, PinSHA: "merge1002", Services: allServices, OnMain: true,
			},
		},
		{
			name: "hotfix carries the run key in its branch",
			setup: func(_ *testing.T, r *deploy.Run) {
				r.Kind, r.Outputs.Digests = deploy.KindHotfix, pinsAt("v0.2.3-beta", "users")
			},
			want: pinResult{
				Title: "Deploy v0.2.3-beta hotfix images", Branch: "chore/deploy-v0.2.3-beta-hotfix-run1", Files: 1,
				PinPR: 1002, PinSHA: "merge1002", Services: allServices, OnMain: true,
			},
		},
		{
			name: "bump names the services it moves",
			setup: func(_ *testing.T, r *deploy.Run) {
				r.Kind, r.TargetSHA = deploy.KindBump, "abcdef1234567890ff"
				r.Outputs.Digests = pinsAt("main-1-abcdef123456", "users", "notifications")
			},
			want: pinResult{
				Title: "chore(deploy): bump notifications and users", Branch: "chore/deploy-bump-abcdef123456-run1", Files: 2,
				PinPR: 1002, PinSHA: "merge1002", Services: []string{"notifications", "users"}, OnMain: true,
			},
		},
		{
			name: "pins already on main are done without a PR",
			setup: func(t *testing.T, r *deploy.Run) {
				r.Outputs.Digests = map[deploy.ImageName]deploy.ImagePin{"users": currentPin(t, "users")}
			},
			want: pinResult{Done: true, PinSHA: "c1", Services: allServices, OnMain: true},
		},
		{
			name:  "no digests is refused",
			setup: func(*testing.T, *deploy.Run) {},
			want:  pinResult{Code: "invalid"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newRun(deploy.KindRelease)
			run.Version = "v0.2.3-beta"
			tc.setup(t, &run)
			f := newFixture(t, run)
			f.gh.commitMain("c1", loadManifests(t))
			done, err := f.runStage(t, deploy.StagePinPR)
			if got := f.pinResult(t, done, err); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("result = %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func currentPin(t *testing.T, img deploy.ImageName) deploy.ImagePin {
	t.Helper()
	return pinsByImage(ParsePins(loadManifests(t), testRepo))[img]
}

func TestPinPRRollback(t *testing.T) {
	cases := []struct {
		name   string
		tagged bool
		want   pinResult
		pinned int
	}{
		{"rolls back to the release builds", true, pinResult{
			Title: "Rollback to v0.2.1-beta", Branch: "chore/rollback-v0.2.1-beta-run1", Files: 15,
			PinPR: 1002, PinSHA: "merge1002", Services: allServices, OnMain: true,
		}, 17},
		{"unknown release is refused", false, pinResult{Code: "invalid"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newRun(deploy.KindRollback)
			run.RollbackTo = "v0.2.1-beta"
			f := newFixture(t, run)
			f.gh.commitMain("c1", loadManifests(t))
			if tc.tagged {
				f.gh.tags["v0.2.1-beta"] = ports.TagRef{Name: "v0.2.1-beta", CommitSHA: "old"}
			}
			noWarp := slices.DeleteFunc(manifestImages(t), func(img deploy.ImageName) bool { return img == "warp" })
			f.publish(noWarp, "v0.2.1-beta", "old")
			done, err := f.runStage(t, deploy.StagePinPR)
			got := f.pinResult(t, done, err)
			if n := len(f.sink.View().Outputs.Digests); !reflect.DeepEqual(got, tc.want) || n != tc.pinned {
				t.Errorf("result = %+v (%d digests)\nwant %+v (%d digests)", got, n, tc.want, tc.pinned)
			}
		})
	}
}
