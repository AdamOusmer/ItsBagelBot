// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"ItsBagelBot/app/deployer/internal/ports"
)

func TestReachable(t *testing.T) {
	down := fake.NewClientset()
	down.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("connection refused")
	})
	cases := []struct {
		name    string
		cs      *fake.Clientset
		wantErr bool
	}{
		{name: "answers", cs: fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}})},
		{name: "refused", cs: down, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := testWatcherOn(c.cs).Reachable(context.Background())
			if c.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestSettled(t *testing.T) {
	full := appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 3, UpdatedReplicas: 3, ReadyReplicas: 3, AvailableReplicas: 3}
	with := func(edit func(*appsv1.DeploymentStatus)) appsv1.DeploymentStatus {
		s := full
		edit(&s)
		return s
	}
	users := ports.WorkloadRef{Kind: kindDeployment, Namespace: "db", Name: "users"}
	leaf := ports.WorkloadRef{Kind: kindDaemonSet, Namespace: "messaging", Name: "nats-leaf"}
	cases := []struct {
		name    string
		obj     runtime.Object
		ref     ports.WorkloadRef
		want    string
		wantErr error
	}{
		{name: "rolled out", obj: depSpec{name: "users", ns: "db", gen: 2, replicas: 3, status: full}.obj(), ref: users},
		{
			name: "generation not observed", ref: users, want: "generation 3 not observed yet (at 2)",
			obj: depSpec{name: "users", ns: "db", gen: 3, replicas: 3, status: full}.obj(),
		},
		{
			name: "mid rollout", ref: users, want: "2/3 updated",
			obj: depSpec{name: "users", ns: "db", gen: 2, replicas: 3, status: with(func(s *appsv1.DeploymentStatus) { s.UpdatedReplicas = 2 })}.obj(),
		},
		{
			name: "not ready", ref: users, want: "2/3 ready",
			obj: depSpec{name: "users", ns: "db", gen: 2, replicas: 3, status: with(func(s *appsv1.DeploymentStatus) { s.ReadyReplicas = 2 })}.obj(),
		},
		{
			name: "old pod draining", ref: users, want: "4 pods for 3 replicas",
			obj: depSpec{name: "users", ns: "db", gen: 2, replicas: 3, status: with(func(s *appsv1.DeploymentStatus) { s.Replicas = 4 })}.obj(),
		},
		{
			name: "daemonset not ready", ref: leaf, want: "2/3 ready",
			obj: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Name: "nats-leaf", Namespace: "messaging", Generation: 1},
				Status: appsv1.DaemonSetStatus{
					ObservedGeneration: 1, DesiredNumberScheduled: 3, CurrentNumberScheduled: 3,
					UpdatedNumberScheduled: 3, NumberReady: 2, NumberAvailable: 2,
				},
			},
		},
		{
			name: "cronjob never unsettled", ref: ports.WorkloadRef{Kind: kindCronJob, Namespace: "db", Name: "digest"},
			obj: &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "digest", Namespace: "db"}},
		},
		{name: "missing workload skipped", obj: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}, ref: users},
		{
			name: "unknown kind", ref: ports.WorkloadRef{Kind: "StatefulSet", Namespace: "messaging", Name: "nats"},
			obj: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}, wantErr: ports.ErrInvalid,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := testWatcher(c.obj).Settled(context.Background(), []ports.WorkloadRef{c.ref})
			if c.wantErr != nil {
				assert.ErrorIs(t, err, c.wantErr)
				return
			}
			require.NoError(t, err)
			var want []ports.Unsettled
			if c.want != "" {
				want = []ports.Unsettled{{Workload: c.ref, Reason: c.want}}
			}
			assert.Equal(t, want, got)
		})
	}
}

func TestLiveImages(t *testing.T) {
	gossip := depSpec{
		name: "gossip", ns: "app", gen: 1, replicas: 1,
		containers: []corev1.Container{{Name: "gossip", Image: testRepo + "/gossip:v0.2.0-beta@sha256:aa"}},
		init:       []corev1.Container{{Name: "warp", Image: testRepo + "/warp:v0.2.2-beta@sha256:bb"}},
	}.obj()
	digest := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "notifications-digest", Namespace: "db"},
		Spec: batchv1.CronJobSpec{JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "digest", Image: testRepo + "/notifications:v0.2.0-beta@sha256:cc"}}},
		}}}},
	}
	gossipRef := ports.WorkloadRef{Kind: kindDeployment, Namespace: "app", Name: "gossip"}
	digestRef := ports.WorkloadRef{Kind: kindCronJob, Namespace: "db", Name: "notifications-digest"}
	got, err := testWatcher(gossip, digest).LiveImages(context.Background(), []ports.WorkloadRef{gossipRef, digestRef})

	require.NoError(t, err)
	assert.Equal(t, []ports.LiveImage{
		{Workload: gossipRef, Container: "gossip", Image: testRepo + "/gossip:v0.2.0-beta@sha256:aa"},
		{Workload: gossipRef, Container: "warp", Image: testRepo + "/warp:v0.2.2-beta@sha256:bb"},
		{Workload: digestRef, Container: "digest", Image: testRepo + "/notifications:v0.2.0-beta@sha256:cc"},
	}, got)
}

func TestVerifyImageIDs(t *testing.T) {
	containers := []corev1.Container{
		{Name: "users", Image: testRepo + "/users:v0.3.0-beta@sha256:new"},
		{Name: "reloader", Image: "docker.io/natsio/nats-server-config-reloader:0.16.0"},
	}
	running := func(id string) []corev1.ContainerStatus {
		return []corev1.ContainerStatus{
			{Name: "users", ImageID: testRepo + "/users@" + id},
			{Name: "reloader", ImageID: "docker.io/natsio/nats-server-config-reloader@sha256:other"},
		}
	}
	pod := func(name string) podSpec {
		return podSpec{name: name, ns: "db", labels: map[string]string{"app": "users"}, containers: containers}
	}
	current, stale, starting, leaving := pod("users-a"), pod("users-b"), pod("users-c"), pod("users-d")
	current.statuses, stale.statuses, leaving.statuses = running("sha256:new"), running("sha256:old"), running("sha256:old")
	leaving.deleting = true
	stranger := podSpec{name: "loyalty-a", ns: "db", labels: map[string]string{"app": "loyalty"}, containers: containers, statuses: running("sha256:old")}

	users := ports.WorkloadRef{Kind: kindDeployment, Namespace: "db", Name: "users"}
	w := testWatcher(
		depSpec{name: "users", ns: "db", gen: 1, replicas: 3, containers: containers}.obj(),
		current.obj(), stale.obj(), starting.obj(), leaving.obj(), stranger.obj(),
	)
	got, err := w.VerifyImageIDs(context.Background(), []ports.WorkloadRef{users}, ports.Pins{"users": "sha256:new"})

	require.NoError(t, err)
	assert.Equal(t, []ports.Mismatch{
		{Workload: users, Pod: "users-b", Container: "users", Want: "sha256:new", Got: "sha256:old"},
		{Workload: users, Pod: "users-c", Container: "users", Want: "sha256:new", Got: ""},
	}, got)
}

func TestVerifyImageIDsReadsEveryImageForm(t *testing.T) {
	users := ports.WorkloadRef{Kind: kindDeployment, Namespace: "db", Name: "users"}
	cases := []struct {
		name    string
		image   string
		imageID string
		want    []ports.Mismatch
	}{
		{
			name: "tag and digest", image: testRepo + "/users:v0.3.0-beta@sha256:new", imageID: testRepo + "/users@sha256:old",
			want: []ports.Mismatch{{Workload: users, Pod: "users-a", Container: "app", Want: "sha256:new", Got: "sha256:old"}},
		},
		{
			name: "digest only", image: testRepo + "/users@sha256:new", imageID: testRepo + "/users@sha256:old",
			want: []ports.Mismatch{{Workload: users, Pod: "users-a", Container: "app", Want: "sha256:new", Got: "sha256:old"}},
		},
		{
			name: "tag only with no digest reported", image: testRepo + "/users:main-1758000000-0123456789ab",
			want: []ports.Mismatch{{Workload: users, Pod: "users-a", Container: "app", Want: "sha256:new"}},
		},
		{name: "image outside the repo", image: "docker.io/natsio/nats-server-config-reloader:0.16.0", imageID: "docker.io/natsio/nats-server-config-reloader@sha256:old"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			containers := []corev1.Container{{Name: "app", Image: tc.image}}
			pod := podSpec{
				name: "users-a", ns: "db", labels: map[string]string{"app": "users"}, containers: containers,
				statuses: []corev1.ContainerStatus{{Name: "app", ImageID: tc.imageID}},
			}
			w := testWatcher(depSpec{name: "users", ns: "db", gen: 1, replicas: 1, containers: containers}.obj(), pod.obj())

			got, err := w.VerifyImageIDs(context.Background(), []ports.WorkloadRef{users}, ports.Pins{"users": "sha256:new"})

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestProbe(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/moved", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/boom", http.StatusFound) })
	mux.HandleFunc("/boom", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := testWatcher()
	cases := map[string]int{"/ok": http.StatusOK, "/moved": http.StatusFound, "/boom": http.StatusInternalServerError}
	for path, want := range cases {
		got, err := w.Probe(context.Background(), ports.URL(srv.URL+path))
		require.NoError(t, err, path)
		assert.Equal(t, want, got, path)
	}
	_, err := w.Probe(context.Background(), "http://127.0.0.1:1/unreachable")
	assert.Error(t, err, "closed port")
}
