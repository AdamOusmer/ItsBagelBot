// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watch

import (
	"net/http"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"ItsBagelBot/app/deployer/internal/ports"
)

const testRepo = "ghcr.io/adamousmer/itsbagelbot"

var testNow = time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

var testCfg = ports.Config{
	ImageRepo:             testRepo,
	RolloutTimeout:        200 * time.Millisecond,
	FailedSchedulingAfter: time.Minute,
	RestartLimit:          3,
	LogTailLines:          50,
	PollEvery:             5 * time.Millisecond,
}

func testWatcher(objs ...runtime.Object) *Watcher {
	w := newWatcher(fake.NewClientset(objs...), testCfg, &http.Client{Timeout: 5 * time.Second})
	w.now = func() time.Time { return testNow }
	return w
}

type depSpec struct {
	name       string
	ns         ports.Namespace
	gen        int64
	replicas   int32
	revision   string
	status     appsv1.DeploymentStatus
	containers []corev1.Container
	init       []corev1.Container
}

func (s depSpec) obj() *appsv1.Deployment {
	labels := map[string]string{"app": s.name}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: s.name, Namespace: string(s.ns), UID: types.UID("uid-" + s.name), Generation: s.gen,
			Annotations: map[string]string{revisionAnnotation: s.revision},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &s.replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: s.containers, InitContainers: s.init},
			},
		},
		Status: s.status,
	}
}

type rsSpec struct {
	dep      *appsv1.Deployment
	hash     string
	revision string
}

func (s rsSpec) obj() *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name: s.dep.Name + "-" + s.hash, Namespace: s.dep.Namespace,
		Labels:          map[string]string{"app": s.dep.Name, appsv1.DefaultDeploymentUniqueLabelKey: s.hash},
		Annotations:     map[string]string{revisionAnnotation: s.revision},
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(s.dep, appsv1.SchemeGroupVersion.WithKind("Deployment"))},
	}}
}

type podSpec struct {
	name       string
	ns         ports.Namespace
	labels     map[string]string
	node       string
	ip         string
	ready      bool
	deleting   bool
	containers []corev1.Container
	statuses   []corev1.ContainerStatus
}

func (s podSpec) obj() *corev1.Pod {
	ready := corev1.ConditionFalse
	if s.ready {
		ready = corev1.ConditionTrue
	}
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: s.name, Namespace: string(s.ns), Labels: s.labels},
		Spec:       corev1.PodSpec{NodeName: s.node, Containers: s.containers},
		Status: corev1.PodStatus{
			PodIP:             s.ip,
			ContainerStatuses: s.statuses,
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: ready}},
		},
	}
	if s.deleting {
		at := metav1.NewTime(testNow)
		p.DeletionTimestamp, p.Finalizers = &at, []string{"test/hold"}
	}
	return p
}

// eventSpec is a core/v1 event about one pod. micro records it the way the
// scheduler does (eventTime only, no firstTimestamp).
type eventSpec struct {
	pod     string
	reason  string
	message string
	age     time.Duration
	micro   bool
}

func (s eventSpec) obj() *corev1.Event {
	e := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: s.pod + "." + s.reason, Namespace: "db"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: s.pod, Namespace: "db"},
		Type:           corev1.EventTypeWarning, Reason: s.reason, Message: s.message,
	}
	at := testNow.Add(-s.age)
	if s.micro {
		e.EventTime = metav1.NewMicroTime(at)
		return e
	}
	e.FirstTimestamp = metav1.NewTime(at)
	return e
}
