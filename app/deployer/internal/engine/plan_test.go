// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const (
	mainSHA deploy.SHA = "aaaaaaaaaaaa1111111111111111111111111111"
	tagSHA  deploy.SHA = "bbbbbbbbbbbb2222222222222222222222222222"

	usersPin  = "ghcr.io/o/r/users:v1.2.3-beta@sha256:01"
	gossipPin = "ghcr.io/o/r/gossip:main-1700000000-aaaaaaaaaaaa@sha256:02"
	gossipRun = "ghcr.io/o/r/gossip:v1.2.3-beta@sha256:00"
	warpPin   = "ghcr.io/o/r/warp:v1.2.3-beta@sha256:03"
	notifyPin = "ghcr.io/o/r/notifications:v1.2.2-beta@sha256:04"
)

var published = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func podSpec(images map[string]string) map[string]any {
	var containers []any
	for name, image := range images {
		containers = append(containers, map[string]any{"name": name, "image": image})
	}
	return map[string]any{"template": map[string]any{"spec": map[string]any{"containers": containers}}}
}

type manifest struct {
	kind, namespace, name string
	images                map[string]string
}

func (m manifest) object() *unstructured.Unstructured {
	spec := podSpec(m.images)
	if m.kind == "CronJob" {
		spec = map[string]any{"jobTemplate": map[string]any{"spec": spec}}
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"kind": m.kind, "metadata": map[string]any{"namespace": m.namespace, "name": m.name}, "spec": spec,
	}}
}

func withCluster(h *harness) {
	h.gh.main = mainSHA
	h.gh.tags = map[deploy.Version]deploy.SHA{"v1.2.3-beta": tagSHA, "v1.2.2-beta": "c"}
	h.gh.releases = []ports.Release{
		{Tag: "v1.2.3-beta", URL: "u3", TargetCommitish: string(tagSHA), PublishedAt: published},
		{Tag: "v1.2.2-beta", URL: "u2", TargetCommitish: "main", PublishedAt: published},
	}
	h.gh.commits = []deploy.Commit{{SHA: mainSHA, Title: "feat: x (#9)", PR: 9}}
	h.gh.prs = []deploy.PRInfo{{Number: 10, Title: "fix: y"}}

	var objs ports.Objects
	for _, m := range []manifest{
		{"Deployment", "db", "users", map[string]string{"users": usersPin}},
		{"Deployment", "app", "gossip", map[string]string{"gossip": gossipPin, "warp": warpPin}},
		{"CronJob", "db", "notifications-digest", map[string]string{"notifications": notifyPin}},
		{"Service", "db", "users", nil},
	} {
		objs = append(objs, m.object())
	}
	h.eng.d.Stage.Applier = fakeApplier{objs: objs}
	h.eng.d.Stage.Watcher = fakeWatcher{live: []ports.LiveImage{
		{Workload: ports.WorkloadRef{Kind: "Deployment", Namespace: "db", Name: "users"}, Container: "users", Image: usersPin},
		{Workload: ports.WorkloadRef{Kind: "Deployment", Namespace: "app", Name: "gossip"}, Container: "gossip", Image: gossipRun},
		{Workload: ports.WorkloadRef{Kind: "Deployment", Namespace: "app", Name: "gossip"}, Container: "warp", Image: warpPin},
		{Workload: ports.WorkloadRef{Kind: "CronJob", Namespace: "db", Name: "notifications-digest"}, Container: "notifications", Image: notifyPin},
	}}
	h.eng.d.Stage.Registry = fakeRegistry{tags: map[deploy.ImageName][]deploy.Tag{
		"users":         {"v1.2.3-beta"},
		"gossip":        {"v1.2.3-beta", "main-1700000000-aaaaaaaaaaaa"},
		"warp":          {"v1.2.3-beta", "main-1600000000-cccccccccccc"},
		"notifications": {"v1.2.2-beta"},
	}}
}

func TestPlanComparesMainWithTheCluster(t *testing.T) {
	cases := []struct {
		name     string
		req      deploy.PlanRequest
		services []string
	}{
		{name: "overview rolls every service", services: testServices},
		{name: "bump rolls what main built", req: deploy.PlanRequest{Kind: deploy.KindBump}, services: []string{"gossip"}},
		{name: "rollback without a target rolls every service", req: deploy.PlanRequest{Kind: deploy.KindRollback}, services: testServices},
		{
			name:     "rollback rolls what the target release built",
			req:      deploy.PlanRequest{Kind: deploy.KindRollback, RollbackTo: "v1.2.3-beta"},
			services: []string{"users", "gossip"},
		},
		{name: "rollback to a release no service built rolls none", req: deploy.PlanRequest{Kind: deploy.KindRollback, RollbackTo: "v1.2.2-beta"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			withCluster(h)

			plan, err := h.eng.Plan(bg, owner, tc.req)
			require.NoError(t, err)

			assert.Equal(t, deploy.Plan{
				LiveVersion: "v1.2.3-beta", LiveSHA: tagSHA, LastTag: "v1.2.3-beta", NextVersion: "v1.2.4-beta",
				MainSHA: mainSHA, InSync: false,
				Drift: []deploy.DriftItem{{
					Namespace: "app", Workload: "gossip", Container: "gossip", Pinned: gossipPin, Live: gossipRun,
				}},
				Commits: h.gh.commits, PRs: h.gh.prs,
				Releases: []deploy.ReleaseInfo{
					{Version: "v1.2.3-beta", SHA: tagSHA, URL: "u3", PublishedAt: published},
					{Version: "v1.2.2-beta", URL: "u2", PublishedAt: published},
				},
				Services: tc.services,
			}, plan)
		})
	}
}

func TestPlanNamesTheActiveRun(t *testing.T) {
	h := newHarness(t)
	withCluster(h)
	seedInterrupted(t, h.store)

	plan, err := h.eng.Plan(bg, owner, deploy.PlanRequest{})
	require.NoError(t, err)

	assert.Equal(t, deploy.RunID("r1"), plan.ActiveRunID)
}

func TestStartRecordsTheLiveRelease(t *testing.T) {
	h := newHarness(t)
	withCluster(h)
	h.boot()

	run := h.await(h.start(deploy.StartRequest{Kind: deploy.KindReapply}).ID, deploy.RunSucceeded)

	assert.Equal(t,
		deploy.Outputs{LiveVersion: "v1.2.3-beta", LiveSHA: tagSHA},
		deploy.Outputs{LiveVersion: run.Outputs.LiveVersion, LiveSHA: run.Outputs.LiveSHA})
}

func TestNextVersionBumpsThePatch(t *testing.T) {
	cases := map[deploy.Version]deploy.Version{
		"v0.2.2-beta":  "v0.2.3-beta",
		"v1.9.9-beta":  "v1.9.10-beta",
		"v1.2.3":       "",
		"v1.2.3-alpha": "",
	}
	got := map[deploy.Version]deploy.Version{}
	for in := range cases {
		got[in] = nextVersion(in)
	}
	assert.Equal(t, cases, got)
}

func TestStartRefusesMalformedRequests(t *testing.T) {
	sha := deploy.SHA(strings.Repeat("a", 40))
	offMain := deploy.SHA(strings.Repeat("b", 40))
	cases := []struct {
		name string
		req  deploy.StartRequest
		want string
	}{
		{"unknown kind", deploy.StartRequest{Kind: "yolo"}, "unknown run kind"},
		{"release needs a version", deploy.StartRequest{Kind: deploy.KindRelease}, "version is required"},
		{"version shape", deploy.StartRequest{Kind: deploy.KindRelease, Version: "1.2.3"}, "is not vX.Y.Z-beta"},
		{"bump takes no version", deploy.StartRequest{Kind: deploy.KindBump, Version: "v1.2.3-beta"}, "version does not apply"},
		{"bump takes no changelog", deploy.StartRequest{Kind: deploy.KindBump, Changelog: &deploy.ChangelogEntry{}}, "changelog does not apply"},
		{"reapply takes no prs", deploy.StartRequest{Kind: deploy.KindReapply, PRs: []int{1}}, "prs does not apply"},
		{"rollback needs a target", deploy.StartRequest{Kind: deploy.KindRollback}, "rollback_to is required"},
		{"short sha", deploy.StartRequest{Kind: deploy.KindReapply, TargetSHA: sha[:12]}, "not a full commit sha"},
		{"target off main", deploy.StartRequest{Kind: deploy.KindBump, TargetSHA: offMain}, "is not on main"},
		{"duplicate pr", deploy.StartRequest{Kind: deploy.KindBump, PRs: []int{4, 4}}, "distinct positive"},
		{"unknown service", deploy.StartRequest{Kind: deploy.KindBump, Services: []string{"nope"}}, `unknown service "nope"`},
		{
			"changelog without english", deploy.StartRequest{
				Kind: deploy.KindRelease, Version: "v1.2.4-beta", Changelog: &deploy.ChangelogEntry{Title: map[string]string{"fr": "x"}},
			}, "English title",
		},
		{"release of an existing tag", deploy.StartRequest{Kind: deploy.KindRelease, Version: "v1.2.3-beta"}, "already exists"},
		{"hotfix of a missing tag", deploy.StartRequest{Kind: deploy.KindHotfix, Version: "v9.9.9-beta"}, "does not exist"},
		{"rollback to a missing release", deploy.StartRequest{Kind: deploy.KindRollback, RollbackTo: "v0.0.1-beta"}, "no release"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			withCluster(h)
			h.gh.offMain = offMain
			h.boot()

			_, err := h.eng.Start(bg, owner, tc.req)

			require.ErrorIs(t, err, ports.ErrInvalid)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

type setClock struct{ now time.Time }

func (c *setClock) Now() time.Time { return c.now }

func TestRepeatPlansReuseTheManifestsOfTheSameCommit(t *testing.T) {
	h := newHarness(t)
	withCluster(h)
	plan := func() {
		_, err := h.eng.Plan(bg, owner, deploy.PlanRequest{})
		require.NoError(t, err)
	}
	plan()
	plan()
	h.gh.main = "f00dfeed"
	plan()
	assert.Equal(t, int32(2), h.gh.trees.Load(), "one tree fetch per main commit")
}

func TestRegistryTagListingsExpireAfterTheTTL(t *testing.T) {
	h := newHarness(t)
	withCluster(h)
	calls := &atomic.Int32{}
	reg := h.eng.d.Stage.Registry.(fakeRegistry)
	reg.calls = calls
	h.eng.d.Stage.Registry = reg
	clock := &setClock{now: time.Unix(1_700_000_000, 0)}
	h.eng.d.Stage.Clock = clock
	bump := func() int32 {
		_, err := h.eng.Plan(bg, owner, deploy.PlanRequest{Kind: deploy.KindBump})
		require.NoError(t, err)
		return calls.Load()
	}
	first := bump()
	require.Positive(t, first)
	cached := bump()
	clock.now = clock.now.Add(tagsTTL)
	assert.Equal(t, [2]int32{first, 2 * first}, [2]int32{cached, bump()})
}
