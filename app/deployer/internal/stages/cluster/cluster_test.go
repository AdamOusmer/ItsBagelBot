// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cluster

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

func TestOrder(t *testing.T) {
	want := []string{
		"commands", "loyalty", "modules", "notifications", "discord-data", "projector", "transactions", "users",
		"discord-engine", "discord-outgress", "gossip", "outgress", "sesame",
		"discord-ingress", "twitch-ingress",
		"console-dashboard", "console-admin",
		"deployer",
	}
	if got := Order(); !slices.Equal(got, want) {
		t.Fatalf("Order() = %v, want %v", got, want)
	}
}

func TestSequence(t *testing.T) {
	cases := []struct {
		name  string
		build ports.Objects
		want  []string
	}{
		{
			name: "known services follow the table, not the build",
			build: ports.Objects{service(nsOps, "deployer"), service(nsApp, "console-admin"),
				service(nsDB, "users"), service(nsApp, "twitch-ingress"), service(nsDB, "commands")},
			want: []string{"commands", "users", "twitch-ingress", "console-admin", "deployer"},
		},
		{
			name: "unknown services close their namespace group, deployer stays last",
			build: ports.Objects{service(nsApp, "zeta"), service("misc", "lone"), service(nsOps, "deployer"),
				service(nsDB, "aardvark"), service(nsApp, "sesame"), service(nsDB, "commands")},
			want: []string{"commands", "aardvark", "sesame", "zeta", "lone", "deployer"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, u := range arrange(tc.build).units {
				got = append(got, u.name)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("rollout order = %v, want %v", got, tc.want)
			}
		})
	}
}

func trainBuild() ports.Objects {
	return ports.Objects{
		object(ports.ObjectRef{Kind: kindPriorityClass, Name: "bagel-high"}),
		object(ports.ObjectRef{Kind: "NetworkPolicy", Namespace: nsApp, Name: "default-deny"}),
		object(ports.ObjectRef{Kind: "DopplerSecret", Namespace: nsDB, Name: "users-doppler"}),
		object(ports.ObjectRef{Kind: kindConfigMap, Namespace: nsDB, Name: "users-env"}),
		service(nsDB, "users"),
		service(nsApp, "sesame"),
		service(nsDB, "commands"),
		service(nsOps, "deployer"),
		object(ports.ObjectRef{Kind: "Service", Namespace: nsOps, Name: "deployer"}),
	}
}

func dbRoutes() ports.Objects {
	return ports.Objects{ingressRoute(ports.ObjectRef{Namespace: nsDB, Name: "service-status"},
		"Host(`health.itsbagelbot.com`) && Path(`/db`)")}
}

func trainHarness(kind deploy.RunKind) *harness {
	h := newHarness(kind)
	h.applier.builds["deploy/k8s"] = trainBuild()
	h.applier.builds["deploy/db"] = dbRoutes()
	return h
}

func TestRolloutRun(t *testing.T) {
	applies := [][]string{
		{"PriorityClass/bagel-high"},
		{"NetworkPolicy/default-deny"},
		{"Deployment/commands"},
		{"ConfigMap/users-env", "Deployment/users"},
		{"Deployment/sesame"},
		{"IngressRoute/service-status"},
	}
	type outcome struct {
		Applied [][]string
		Waited  []string
		Err     string
		Rows    map[string]string
	}
	cases := []struct {
		name        string
		cancelAfter int
		want        outcome
	}{
		{
			name: "rolls one service at a time in order, deployer skipped",
			want: outcome{Applied: applies, Waited: []string{"commands", "users", "sesame"}, Rows: map[string]string{
				"priority": "succeeded 0/0", "shared": "succeeded 0/0", "commands": "succeeded 1/1",
				"users": "succeeded 1/1", "sesame": "succeeded 1/1", "deployer": "skipped 0/0",
				"status-routes": "succeeded 0/0",
			}},
		},
		{
			name:        "cancel stops before the next service",
			cancelAfter: 1,
			want: outcome{Applied: applies[:3], Waited: []string{"commands"}, Err: "cancelled", Rows: map[string]string{
				"priority": "succeeded 0/0", "shared": "succeeded 0/0", "commands": "succeeded 1/1",
				"users": "cancelled 0/0", "sesame": "pending 0/0", "deployer": "pending 0/0",
				"status-routes": "pending 0/0",
			}},
		},
		{
			name:        "cancel after the last service leaves the status routes unapplied",
			cancelAfter: 3,
			want: outcome{Applied: applies[:5], Waited: []string{"commands", "users", "sesame"}, Err: "cancelled", Rows: map[string]string{
				"priority": "succeeded 0/0", "shared": "succeeded 0/0", "commands": "succeeded 1/1",
				"users": "succeeded 1/1", "sesame": "succeeded 1/1", "deployer": "cancelled 0/0",
				"status-routes": "pending 0/0",
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := trainHarness(deploy.KindRelease)
			h.watcher.afterWait = func(n int) {
				if n == tc.cancelAfter {
					h.sink.cancel()
				}
			}
			err := rollout{}.Run(context.Background(), h.rc(deploy.StageRollout))
			got := outcome{Applied: h.applier.applied, Waited: h.watcher.waited, Err: errCode(err),
				Rows: h.sink.rows(deploy.StageRollout)}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("rollout =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func TestRolloutResumedCancel(t *testing.T) {
	nothingApplied := liveTrain()
	for i := range nothingApplied {
		nothingApplied[i].Image = testRepo + "/old:v0@sha256:old"
	}
	type outcome struct {
		Applied [][]string
		Waited  []string
		Err     string
	}
	cases := []struct {
		name string
		live []ports.LiveImage
		want outcome
	}{
		{"waits on the service in flight", liveTrain(), outcome{Waited: []string{"commands"}, Err: "cancelled"}},
		{"nothing in flight stops at once", nothingApplied, outcome{Err: "cancelled"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := trainHarness(deploy.KindRelease)
			h.watcher.live, h.watcher.unsettled = tc.live, []ports.Unsettled{{Reason: "1 of 2 updated"}}
			h.sink.cancel()
			err := rollout{}.Run(context.Background(), h.rc(deploy.StageRollout))
			got := outcome{Applied: h.applier.applied, Waited: h.watcher.waited, Err: errCode(err)}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resumed cancel = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func liveTrain() []ports.LiveImage {
	var live []ports.LiveImage
	for _, s := range []struct {
		ns   ports.Namespace
		name deploy.ImageName
	}{{nsDB, "commands"}, {nsDB, "users"}, {nsApp, "sesame"}} {
		ref := ports.WorkloadRef{Kind: kindDeployment, Namespace: s.ns, Name: string(s.name)}
		live = append(live, ports.LiveImage{Workload: ref, Container: string(s.name), Image: imageOf(s.name)})
	}
	return live
}

func TestRolloutDone(t *testing.T) {
	stale := liveTrain()
	stale[1].Image = testRepo + "/users:v0@sha256:old"
	cases := []struct {
		name      string
		kind      deploy.RunKind
		lastRow   deploy.StageState
		live      []ports.LiveImage
		unsettled []ports.Unsettled
		want      bool
	}{
		{name: "every step ran, pinned images live and settled", kind: deploy.KindRelease, lastRow: deploy.StateSucceeded, live: liveTrain(), want: true},
		{name: "status routes failed after every service rolled", kind: deploy.KindRelease, lastRow: deploy.StateFailed, live: liveTrain()},
		{name: "no earlier attempt reached the end", kind: deploy.KindRelease, live: liveTrain()},
		{name: "a service still on the old image", kind: deploy.KindRelease, lastRow: deploy.StateSucceeded, live: stale},
		{name: "a service missing from the cluster", kind: deploy.KindRelease, lastRow: deploy.StateSucceeded, live: liveTrain()[:2]},
		{name: "a service mid-rollout", kind: deploy.KindRelease, lastRow: deploy.StateSucceeded, live: liveTrain(),
			unsettled: []ports.Unsettled{{Reason: "1 of 2 updated"}}},
		{name: "a reapply always rolls", kind: deploy.KindReapply, lastRow: deploy.StateSucceeded, live: liveTrain()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := trainHarness(tc.kind)
			h.watcher.live, h.watcher.unsettled = tc.live, tc.unsettled
			if tc.lastRow != "" {
				_ = h.sink.Update(context.Background(), func(r *deploy.Run) {
					r.Stage(deploy.StageRollout).Items = []deploy.Item{{Key: "users", State: deploy.StateSucceeded}, {Key: "status-routes", State: tc.lastRow}}
				})
			}
			done, err := rollout{}.Done(context.Background(), h.rc(deploy.StageRollout))
			if err != nil || done != tc.want {
				t.Fatalf("Done() = %v, %v; want %v", done, err, tc.want)
			}
		})
	}
}

func TestACL(t *testing.T) {
	messaging := ports.Objects{
		object(ports.ObjectRef{Kind: kindConfigMap, Namespace: nsMessaging, Name: "nats-config"}),
		object(ports.ObjectRef{Kind: "Certificate", Namespace: nsMessaging, Name: "nats-cert"}),
	}
	touched := []ports.FilePath{"deploy/messaging/nats.conf"}
	fresh := ports.NATSServer{Pod: "nats-0", Node: "node1", ConfigLoaded: t0.Add(time.Second)}
	stale := ports.NATSServer{Pod: "nats-leaf-x", Node: "node2", ConfigLoaded: t0.Add(-time.Hour)}
	type outcome struct {
		Err       string
		Applied   int
		Changed   bool
		AppliedAt bool
		Rows      map[string]string
	}
	cases := []struct {
		name    string
		files   []ports.FilePath
		changed []ports.ObjectRef
		servers []ports.NATSServer
		want    outcome
	}{
		{
			name:  "messaging unchanged skips without applying",
			files: []ports.FilePath{"deploy/k8s/users.yaml"},
			want:  outcome{Err: "skipped", Rows: map[string]string{}},
		},
		{
			name: "an apply that changes nothing the servers load waits on no reload", files: touched,
			changed: []ports.ObjectRef{{Kind: "Service"}},
			want:    outcome{Applied: 1, Changed: true, Rows: map[string]string{"reload": "skipped 0/0"}},
		},
		{
			name: "every server reloaded after the apply", files: touched,
			changed: []ports.ObjectRef{{Kind: kindConfigMap}},
			servers: []ports.NATSServer{fresh, {Pod: "nats-leaf-y", Node: "node2", ConfigLoaded: t0.Add(2 * time.Second)}},
			want: outcome{Applied: 1, Changed: true, AppliedAt: true,
				Rows: map[string]string{"nats-0": "succeeded 1/1", "nats-leaf-y": "succeeded 1/1"}},
		},
		{
			name: "a server that never reloads times the stage out", files: touched,
			changed: []ports.ObjectRef{{Kind: kindConfigMap}},
			servers: []ports.NATSServer{fresh, stale},
			want: outcome{Err: "timeout", Applied: 1, Changed: true, AppliedAt: true,
				Rows: map[string]string{"nats-0": "succeeded 1/1", "nats-leaf-x": "running 0/1"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(deploy.KindRelease)
			h.applier.builds["deploy/messaging"] = messaging
			h.gh.changed, h.applier.changed, h.watcher.servers = tc.files, tc.changed, tc.servers
			err := acl{}.Run(context.Background(), h.rc(deploy.StageACL))
			out := h.sink.View().Outputs
			got := outcome{Err: errCode(err), Applied: len(h.applier.applied), Changed: out.MessagingChanged,
				AppliedAt: out.ACLAppliedAt != nil, Rows: h.sink.rows(deploy.StageACL)}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("acl =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func TestACLAppliesManagedObjectsOnly(t *testing.T) {
	h := newHarness(deploy.KindRelease)
	h.applier.builds["deploy/messaging"] = ports.Objects{
		object(ports.ObjectRef{Kind: kindConfigMap, Namespace: nsMessaging, Name: "nats-config"}),
		object(ports.ObjectRef{Kind: "Certificate", Namespace: nsMessaging, Name: "nats-cert"}),
		object(ports.ObjectRef{Kind: "DopplerSecret", Namespace: nsMessaging, Name: "nats-doppler"}),
	}
	h.gh.changed = []ports.FilePath{"deploy/messaging/kustomization.yaml"}
	_ = acl{}.Run(context.Background(), h.rc(deploy.StageACL))
	want := [][]string{{"ConfigMap/nats-config"}}
	if !reflect.DeepEqual(h.applier.applied, want) {
		t.Fatalf("applied %v, want %v", h.applier.applied, want)
	}
}

func verifyBuild() ports.Objects {
	return append(trainBuild(),
		ingressRoute(ports.ObjectRef{Namespace: nsApp, Name: "console-dashboard"},
			"(Host(`dashboard.itsbagelbot.com`) || Host(`stats.itsbagelbot.com`)) && (Path(`/status`) || Path(`/healthz`))",
			"(Host(`dashboard.itsbagelbot.com`) || Host(`stats.itsbagelbot.com`)) && PathPrefix(`/auth/`)",
			"Host(`dashboard.itsbagelbot.com`) || Host(`stats.itsbagelbot.com`)"),
		ingressRoute(ports.ObjectRef{Namespace: nsApp, Name: "unrouted-host-fallback"}, "HostRegexp(`^.+$`)"),
	)
}

func TestProbeURLs(t *testing.T) {
	objs := append(verifyBuild(),
		ingressRoute(ports.ObjectRef{Namespace: nsDB, Name: "transactions-webhooks"},
			"Host(`webhooks.itsbagelbot.com`) && (PathPrefix(`/tebex`) || PathPrefix(`/webhooks/tebex`))"),
		ingressRoute(ports.ObjectRef{Namespace: nsApp, Name: "service-status"},
			"Host(`health.itsbagelbot.com`) && Path(`/twitch`)", "Host(`health.itsbagelbot.com`) && Path(`/discord`)"),
	)
	want := []ports.URL{
		"https://dashboard.itsbagelbot.com/status",
		"https://stats.itsbagelbot.com/status",
		"https://webhooks.itsbagelbot.com/tebex",
		"https://health.itsbagelbot.com/twitch",
	}
	if got := probeURLs(objs); !slices.Equal(got, want) {
		t.Fatalf("probeURLs = %v, want %v", got, want)
	}
}

func TestVerify(t *testing.T) {
	healthy := map[ports.URL]int{
		"https://dashboard.itsbagelbot.com/status": 200,
		"https://stats.itsbagelbot.com/status":     302,
		"https://health.itsbagelbot.com/db":        200,
	}
	type outcome struct {
		Err     string
		Actions []deploy.Action
		LogTail []string
		Rows    map[string]string
	}
	rows := func(failed ...string) map[string]string {
		out := map[string]string{}
		for _, k := range []string{"image:commands", "image:users", "image:sesame"} {
			out[k] = "succeeded 0/0"
		}
		for url := range healthy {
			out[string(url)] = "succeeded 0/0"
		}
		for _, k := range failed {
			out[k] = "failed 0/0"
		}
		return out
	}
	rollback := []deploy.Action{deploy.ActionRollback}
	cases := []struct {
		name  string
		off   map[string][]ports.Mismatch
		codes map[ports.URL]int
		want  outcome
	}{
		{name: "pins live and hosts answering", codes: healthy, want: outcome{Rows: rows()}},
		{
			name: "a pod off its pin fails with rollback offered", codes: healthy,
			off: map[string][]ports.Mismatch{"users": {{
				Workload: ports.WorkloadRef{Kind: kindDeployment, Namespace: nsDB, Name: "users"},
				Pod:      "users-0", Container: "users", Want: "sha256:users", Got: "sha256:old",
			}}},
			want: outcome{Err: "verify_failed", Actions: rollback, Rows: rows("image:users"),
				LogTail: []string{"db/users pod users-0 container users runs sha256:old, pinned sha256:users"}},
		},
		{
			name: "every failing host is reported, not just the first",
			codes: map[ports.URL]int{
				"https://dashboard.itsbagelbot.com/status": 200,
				"https://health.itsbagelbot.com/db":        503,
			},
			want: outcome{Err: "verify_failed", Actions: rollback,
				Rows: rows("https://stats.itsbagelbot.com/status", "https://health.itsbagelbot.com/db"),
				LogTail: []string{
					"https://stats.itsbagelbot.com/status: connection refused",
					"https://health.itsbagelbot.com/db: answered 503",
				}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(deploy.KindRelease)
			h.applier.builds["deploy/k8s"] = verifyBuild()
			h.applier.builds["deploy/db"] = dbRoutes()
			h.watcher.off, h.watcher.codes = tc.off, tc.codes
			err := verify{}.Run(context.Background(), h.rc(deploy.StageVerify))
			got := outcome{Err: errCode(err), Rows: h.sink.rows(deploy.StageVerify)}
			if f, ok := ports.AsFail(err); ok {
				got.Actions, got.LogTail = f.Actions, f.LogTail
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("verify =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}
