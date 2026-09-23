// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watch

import (
	"cmp"
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// rolloutOutcome is everything one WaitRollout call is judged on, compared
// in one go.
type rolloutOutcome struct {
	Code     deploy.FailureCode
	Canceled bool
	Last     deploy.Item
	Tail     []string
}

type rolloutCase struct {
	name     string
	kind     string
	status   appsv1.DeploymentStatus
	pods     []podSpec
	events   []eventSpec
	canceled bool
	// revision is the Deployment's revision annotation, "2" (the new
	// ReplicaSet) unless the case predates the controller's first sync.
	revision string
	want     rolloutOutcome
}

var (
	rolledOut = appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2}
	halfway   = appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 2, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1}
)

func commandsPod(name, hash string) podSpec {
	return podSpec{
		name: name, ns: "db",
		labels:     map[string]string{"app": "commands", appsv1.DefaultDeploymentUniqueLabelKey: hash},
		containers: []corev1.Container{{Name: "commands", Image: testRepo + "/commands:v0.3.0-beta@sha256:new"}},
	}
}

func placed(p podSpec, node string, ready bool, statuses ...corev1.ContainerStatus) podSpec {
	p.node, p.ready, p.statuses = node, ready, statuses
	return p
}

func waitingStatus(reason string, restarts int32) corev1.ContainerStatus {
	return corev1.ContainerStatus{
		Name: "commands", RestartCount: restarts,
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: "back-off"}},
	}
}

// The rollout fixtures: a two-replica commands Deployment moving from
// ReplicaSet "old" to "new" across node1 and node2.
var (
	oldA     = placed(commandsPod("commands-old-a", "old"), "node1", true)
	newA     = placed(commandsPod("commands-new-a", "new"), "node1", true)
	newB     = placed(commandsPod("commands-new-b", "new"), "node2", true)
	pendingB = commandsPod("commands-new-b", "new")

	oldDot     = deploy.NodePod{Node: "node1", Pod: "commands-old-a", Phase: deploy.PodOld}
	pendingDot = deploy.NodePod{Pod: "commands-new-b", Phase: deploy.PodPending}
	failingDot = deploy.NodePod{Node: "node2", Pod: "commands-new-b", Phase: deploy.PodFailing}

	antiAffinity = "0/3 nodes are available: 2 node(s) didn't match pod anti-affinity rules, 1 node(s) had untolerated taint."
	timedOut     = "commands: rollout incomplete at the deadline: 1/2 updated"
	unsynced     = "commands: rollout incomplete at the deadline: generation 2 not observed yet (at 1)"
)

// failedHalfway is the outcome of a rollout that fails with one of its two
// replicas not yet new and ready: f's message is the item's detail.
func failedHalfway(f deploy.Failure, nodes ...deploy.NodePod) rolloutOutcome {
	last := deploy.Item{State: deploy.StateFailed, Progress: deploy.Progress{Total: 2}, Detail: f.Message, Nodes: nodes}
	return rolloutOutcome{Code: f.Code, Last: last, Tail: f.LogTail}
}

// fastFailCases stop the wait early on what the timeout cannot fix.
func fastFailCases() []rolloutCase {
	return []rolloutCase{
		{
			name: "image pull", status: halfway,
			pods:   []podSpec{oldA, placed(newB, "node2", false, waitingStatus("ImagePullBackOff", 0))},
			events: []eventSpec{{pod: "commands-new-b", reason: "Failed", message: "Failed to pull image: not found", age: 30 * time.Second}},
			want:   failedHalfway(deploy.Failure{Code: deploy.FailImagePull, Message: "commands: pod commands-new-b: container commands: ImagePullBackOff: back-off", LogTail: []string{"2026-09-22T11:59:30Z Warning Failed: Failed to pull image: not found"}}, oldDot, failingDot),
		},
		{
			name: "crash loop", status: halfway,
			pods:   []podSpec{oldA, placed(newB, "node2", false, waitingStatus("CrashLoopBackOff", 3))},
			events: []eventSpec{{pod: "commands-new-b", reason: "BackOff", message: "Back-off restarting failed container", age: 10 * time.Second}},
			want:   failedHalfway(deploy.Failure{Code: deploy.FailCrashLoop, Message: "commands: pod commands-new-b: container commands restarted 3 times", LogTail: []string{"2026-09-22T11:59:50Z Warning BackOff: Back-off restarting failed container", "log of commands:", "fake logs"}}, oldDot, failingDot),
		},
		{
			name: "anti-affinity past the grace", status: halfway, pods: []podSpec{oldA, pendingB},
			events: []eventSpec{{pod: "commands-new-b", reason: "FailedScheduling", message: antiAffinity, age: 2 * time.Minute, micro: true}},
			want:   failedHalfway(deploy.Failure{Code: deploy.FailUnschedulable, Message: "commands: pod commands-new-b: " + antiAffinity, LogTail: []string{"2026-09-22T11:58:00Z Warning FailedScheduling: " + antiAffinity}}, pendingDot, oldDot),
		},
	}
}

// waitCases finish, run into the timeout or are cancelled.
func waitCases() []rolloutCase {
	return []rolloutCase{
		{
			name: "rolled out", status: rolledOut, pods: []podSpec{newA, newB},
			want: rolloutOutcome{Last: deploy.Item{State: deploy.StateSucceeded, Progress: deploy.Progress{Done: 2, Total: 2}, Nodes: []deploy.NodePod{
				{Node: "node1", Pod: "commands-new-a", Phase: deploy.PodNew}, {Node: "node2", Pod: "commands-new-b", Phase: deploy.PodNew},
			}}},
		},
		{
			name: "anti-affinity inside the grace times out", status: halfway, pods: []podSpec{oldA, pendingB},
			events: []eventSpec{{pod: "commands-new-b", reason: "FailedScheduling", message: antiAffinity, age: 10 * time.Second, micro: true}},
			want:   failedHalfway(deploy.Failure{Code: deploy.FailTimeout, Message: timedOut, LogTail: []string{"2026-09-22T11:59:50Z Warning FailedScheduling: " + antiAffinity}}, pendingDot, oldDot),
		},
		{
			name: "short on cpu is left to the timeout", status: halfway, pods: []podSpec{oldA, pendingB},
			events: []eventSpec{{pod: "commands-new-b", reason: "FailedScheduling", message: "0/3 nodes are available: 3 Insufficient cpu.", age: 2 * time.Minute}},
			want:   failedHalfway(deploy.Failure{Code: deploy.FailTimeout, Message: timedOut, LogTail: []string{"2026-09-22T11:58:00Z Warning FailedScheduling: 0/3 nodes are available: 3 Insufficient cpu."}}, pendingDot, oldDot),
		},
		{
			name: "cancel passes through", status: halfway, canceled: true,
			pods: []podSpec{oldA, placed(newB, "node2", false)},
			want: rolloutOutcome{Canceled: true, Last: deploy.Item{
				State: deploy.StateRunning, Progress: deploy.Progress{Total: 2},
				Nodes: []deploy.NodePod{oldDot, {Node: "node2", Pod: "commands-new-b", Phase: deploy.PodPending}},
			}},
		},
		{
			// Right after the apply the controller has not synced: the
			// revision annotation still names the old ReplicaSet, whose
			// long-lived pod carries restarts from earlier NATS blips.
			name: "old pod restarts before the controller syncs are not a crash loop", revision: "1",
			status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2},
			pods:   []podSpec{placed(commandsPod("commands-old-a", "old"), "node1", true, corev1.ContainerStatus{Name: "commands", RestartCount: 5})},
			want: rolloutOutcome{Code: deploy.FailTimeout, Last: deploy.Item{
				State: deploy.StateFailed, Progress: deploy.Progress{Done: 1, Total: 2}, Detail: unsynced,
				Nodes: []deploy.NodePod{{Node: "node1", Pod: "commands-old-a", Phase: deploy.PodNew}},
			}},
		},
		{name: "cronjob has nothing to roll", kind: kindCronJob},
	}
}

func (c rolloutCase) objects() []runtime.Object {
	dep := depSpec{
		name: "commands", ns: "db", gen: 2, replicas: 2, revision: cmp.Or(c.revision, "2"), status: c.status,
		containers: []corev1.Container{{Name: "commands", Image: testRepo + "/commands:v0.3.0-beta@sha256:new"}},
	}.obj()
	objs := []runtime.Object{dep, rsSpec{dep: dep, hash: "new", revision: "2"}.obj(), rsSpec{dep: dep, hash: "old", revision: "1"}.obj()}
	for _, p := range c.pods {
		objs = append(objs, p.obj())
	}
	for _, e := range c.events {
		objs = append(objs, e.obj())
	}
	return objs
}

func TestWaitRollout(t *testing.T) {
	for _, c := range slices.Concat(fastFailCases(), waitCases()) {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if c.canceled {
				cancel()
			}
			kind := c.kind
			if kind == "" {
				kind = kindDeployment
			}
			var last deploy.Item
			spec := ports.RolloutSpec{Workload: ports.WorkloadRef{Kind: kind, Namespace: "db", Name: "commands"}}
			err := testWatcher(c.objects()...).WaitRollout(ctx, spec, func(it deploy.Item) { last = it })

			got := rolloutOutcome{Canceled: errors.Is(err, context.Canceled), Last: last}
			if f, ok := ports.AsFail(err); ok {
				got.Code, got.Tail = f.Code, f.LogTail
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("WaitRollout() = %+v (err %v)\nwant %+v", got, err, c.want)
			}
		})
	}
}

// TestWaitRolloutReportsOnChange pins that progress fires once per distinct
// observation, not once per poll: a rollout that sits unchanged for the
// whole timeout reports running once, then failed.
func TestWaitRolloutReportsOnChange(t *testing.T) {
	c := rolloutCase{status: halfway, pods: []podSpec{oldA, pendingB}}
	var states []deploy.StageState
	spec := ports.RolloutSpec{Workload: ports.WorkloadRef{Kind: kindDeployment, Namespace: "db", Name: "commands"}}
	_ = testWatcher(c.objects()...).WaitRollout(context.Background(), spec, func(it deploy.Item) { states = append(states, it.State) })
	if want := []deploy.StageState{deploy.StateRunning, deploy.StateFailed}; !reflect.DeepEqual(states, want) {
		t.Fatalf("progress states = %v, want %v", states, want)
	}
}
