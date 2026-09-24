// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/redact"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const revisionAnnotation = "deployment.kubernetes.io/revision"

const failedSchedulingSelector = "involvedObject.kind=Pod,reason=FailedScheduling"

const tailTimeout = 10 * time.Second

var pullReasons = map[string]bool{"ImagePullBackOff": true, "ErrImagePull": true}

var failingReasons = map[string]bool{
	"CrashLoopBackOff": true, "ImagePullBackOff": true, "ErrImagePull": true,
	"CreateContainerConfigError": true, "CreateContainerError": true,
	"InvalidImageName": true, "RunContainerError": true,
}

func (w *Watcher) WaitRollout(ctx context.Context, spec ports.RolloutSpec, progress ports.ProgressFunc) error {
	switch spec.Workload.Kind {
	case kindCronJob:
		return nil
	case kindDeployment:
		ctx, cancel := context.WithTimeout(ctx, w.cfg.RolloutTimeout)
		defer cancel()
		r := &rollout{w: w, ref: spec.Workload, progress: progress}
		return r.run(ctx)
	}
	return fmt.Errorf("%w: cannot wait on a %s rollout", ports.ErrInvalid, spec.Workload.Kind)
}

type rollout struct {
	w        *Watcher
	ref      ports.WorkloadRef
	progress ports.ProgressFunc
	view     view
	last     deploy.Item
}

type view struct {
	dep  *appsv1.Deployment
	hash string
	pods []corev1.Pod
}

type trigger struct {
	code      deploy.FailureCode
	pod       *corev1.Pod
	container string
	previous  bool
	message   string
}

func (r *rollout) run(ctx context.Context) error {
	tick := time.NewTicker(r.w.cfg.PollEvery)
	defer tick.Stop()
	for {
		done, err := r.step(ctx)
		if err != nil {
			return r.fail(ctx, err)
		}
		if done {
			r.emit(deploy.StateSucceeded, "")
			return nil
		}
		select {
		case <-ctx.Done():
			return r.fail(ctx, ctx.Err())
		case <-tick.C:
		}
	}
}

func (r *rollout) step(ctx context.Context) (bool, error) {
	v, err := r.observe(ctx)
	if err != nil {
		return false, err
	}
	r.view = v
	r.emit(deploy.StateRunning, "")
	if deploymentUnsettled(v.dep) == "" {
		return true, nil
	}
	if !v.observed() {
		return false, nil
	}
	return false, r.check(ctx)
}

func (v view) observed() bool {
	if v.dep.Status.ObservedGeneration < v.dep.Generation {
		return false
	}
	return v.hash != ""
}

func (r *rollout) observe(ctx context.Context) (view, error) {
	d, err := r.w.cs.AppsV1().Deployments(string(r.ref.Namespace)).Get(ctx, r.ref.Name, metav1.GetOptions{})
	if err != nil {
		return view{}, fmt.Errorf("get deployment: %w", err)
	}
	hash, err := r.newHash(ctx, d)
	if err != nil {
		return view{}, err
	}
	pods, err := r.w.pods(ctx, r.ref.Namespace, d.Spec.Selector)
	return view{dep: d, hash: hash, pods: pods}, err
}

func (r *rollout) newHash(ctx context.Context, d *appsv1.Deployment) (string, error) {
	sel, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return "", fmt.Errorf("selector: %w", err)
	}
	list, err := r.w.cs.AppsV1().ReplicaSets(d.Namespace).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return "", fmt.Errorf("list replicasets: %w", err)
	}
	i := slices.IndexFunc(list.Items, func(rs appsv1.ReplicaSet) bool { return isNewReplicaSet(&rs, d) })
	if i < 0 {
		return "", nil
	}
	return list.Items[i].Labels[appsv1.DefaultDeploymentUniqueLabelKey], nil
}

func isNewReplicaSet(rs *appsv1.ReplicaSet, d *appsv1.Deployment) bool {
	if !metav1.IsControlledBy(rs, d) {
		return false
	}
	rev := d.Annotations[revisionAnnotation]
	return rev != "" && rs.Annotations[revisionAnnotation] == rev
}

func (r *rollout) check(ctx context.Context) error {
	fresh := r.view.newPods()
	if t, ok := firstTrouble(fresh, r.w.cfg.RestartLimit); ok {
		return r.failure(ctx, t)
	}
	t, ok, err := r.unschedulable(ctx, fresh)
	if err != nil || !ok {
		return err
	}
	return r.failure(ctx, t)
}

func firstTrouble(pods []corev1.Pod, limit int32) (trigger, bool) {
	for i := range pods {
		statuses := containerStatuses(&pods[i])
		for j := range statuses {
			if t, ok := statusTrouble(&statuses[j], limit); ok {
				t.pod = &pods[i]
				return t, true
			}
		}
	}
	return trigger{}, false
}

func statusTrouble(s *corev1.ContainerStatus, limit int32) (trigger, bool) {
	if w := s.State.Waiting; w != nil && pullReasons[w.Reason] {
		return trigger{code: deploy.FailImagePull, message: fmt.Sprintf("container %s: %s: %s", s.Name, w.Reason, w.Message)}, true
	}
	if s.RestartCount >= limit {
		msg := fmt.Sprintf("container %s restarted %d times", s.Name, s.RestartCount)
		return trigger{code: deploy.FailCrashLoop, container: s.Name, previous: true, message: msg}, true
	}
	return trigger{}, false
}

func (r *rollout) unschedulable(ctx context.Context, fresh []corev1.Pod) (trigger, bool, error) {
	waiting := unscheduled(fresh)
	if len(waiting) == 0 {
		return trigger{}, false, nil
	}
	events, err := r.w.cs.CoreV1().Events(string(r.ref.Namespace)).List(ctx, metav1.ListOptions{FieldSelector: failedSchedulingSelector})
	if err != nil {
		return trigger{}, false, fmt.Errorf("list events: %w", err)
	}
	cutoff := r.w.now().Add(-r.w.cfg.FailedSchedulingAfter)
	for i := range events.Items {
		e := &events.Items[i]
		pod, ok := waiting[e.InvolvedObject.Name]
		if ok && stuckScheduling(e, cutoff) {
			return trigger{code: deploy.FailUnschedulable, pod: pod, message: e.Message}, true, nil
		}
	}
	return trigger{}, false, nil
}

func unscheduled(pods []corev1.Pod) map[string]*corev1.Pod {
	out := map[string]*corev1.Pod{}
	for i := range pods {
		if pods[i].Spec.NodeName == "" {
			out[pods[i].Name] = &pods[i]
		}
	}
	return out
}

func stuckScheduling(e *corev1.Event, cutoff time.Time) bool {
	if e.Reason != "FailedScheduling" || !eventStart(e).Before(cutoff) {
		return false
	}
	return blockedBySpread(e.Message)
}

func blockedBySpread(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "anti-affinity") || strings.Contains(m, "topology spread")
}

func eventStart(e *corev1.Event) time.Time {
	switch {
	case !e.FirstTimestamp.IsZero():
		return e.FirstTimestamp.Time
	case !e.EventTime.IsZero():
		return e.EventTime.Time
	}
	return e.CreationTimestamp.Time
}

func (r *rollout) failure(ctx context.Context, t trigger) error {
	f := ports.Failf(t.code, "%s: pod %s: %s", r.ref.Name, t.pod.Name, t.message)
	f.LogTail = r.w.tail(ctx, t)
	return f
}

func (r *rollout) fail(ctx context.Context, err error) error {
	f, ok := ports.AsFail(err)
	switch {
	case ok:
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		f = r.timeout(ctx)
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		f = ports.Failf(deploy.FailKube, "%s: %v", r.ref.Name, err)
	}
	r.emit(deploy.StateFailed, f.Message)
	return f
}

func (r *rollout) timeout(ctx context.Context) *ports.Fail {
	f := ports.Failf(deploy.FailTimeout, "%s: rollout incomplete at the deadline: %s", r.ref.Name, r.view.pending())
	t, ok := stalled(r.view.newPods())
	if !ok {
		return f
	}
	tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tailTimeout)
	defer cancel()
	f.LogTail = r.w.tail(tctx, t)
	return f
}

func stalled(pods []corev1.Pod) (trigger, bool) {
	i := slices.IndexFunc(pods, func(p corev1.Pod) bool { return !podReady(&p) })
	if i < 0 {
		return trigger{}, false
	}
	return trigger{pod: &pods[i], container: notReadyContainer(&pods[i])}, true
}

func notReadyContainer(p *corev1.Pod) string {
	i := slices.IndexFunc(p.Status.ContainerStatuses, func(s corev1.ContainerStatus) bool { return !s.Ready })
	if i < 0 {
		return ""
	}
	return p.Status.ContainerStatuses[i].Name
}

func (r *rollout) emit(state deploy.StageState, detail string) {
	item := r.view.item()
	item.State, item.Detail = state, detail
	if reflect.DeepEqual(item, r.last) {
		return
	}
	r.last = item
	r.progress(item)
}

func (v view) pending() string {
	if v.dep == nil {
		return "deployment never read"
	}
	return deploymentUnsettled(v.dep)
}

func (v view) isNew(p *corev1.Pod) bool {
	return v.hash != "" && p.Labels[appsv1.DefaultDeploymentUniqueLabelKey] == v.hash
}

func (v view) newPods() []corev1.Pod {
	var out []corev1.Pod
	for i := range v.pods {
		if v.isNew(&v.pods[i]) {
			out = append(out, v.pods[i])
		}
	}
	return out
}

func (v view) item() deploy.Item {
	if v.dep == nil {
		return deploy.Item{}
	}
	ready := 0
	nodes := make([]deploy.NodePod, 0, len(v.pods))
	for i := range v.pods {
		p := &v.pods[i]
		phase := v.phase(p)
		if phase == deploy.PodNew {
			ready++
		}
		nodes = append(nodes, deploy.NodePod{Node: p.Spec.NodeName, Pod: p.Name, Phase: phase})
	}
	slices.SortStableFunc(nodes, func(a, b deploy.NodePod) int { return cmp.Compare(a.Node, b.Node) })
	return deploy.Item{Progress: deploy.Progress{Done: ready, Total: int(desired(v.dep.Spec.Replicas))}, Nodes: nodes}
}

func (v view) phase(p *corev1.Pod) deploy.PodPhase {
	switch {
	case !v.isNew(p):
		return deploy.PodOld
	case podFailing(p):
		return deploy.PodFailing
	case podReady(p):
		return deploy.PodNew
	}
	return deploy.PodPending
}

func podReady(p *corev1.Pod) bool {
	i := slices.IndexFunc(p.Status.Conditions, func(c corev1.PodCondition) bool { return c.Type == corev1.PodReady })
	return i >= 0 && p.Status.Conditions[i].Status == corev1.ConditionTrue
}

func podFailing(p *corev1.Pod) bool {
	return slices.ContainsFunc(containerStatuses(p), func(s corev1.ContainerStatus) bool {
		return s.State.Waiting != nil && failingReasons[s.State.Waiting.Reason]
	})
}

// tail must redact every line: it is persisted in run history.
func (w *Watcher) tail(ctx context.Context, t trigger) []string {
	lines := w.podEvents(ctx, t.pod)
	if t.container != "" {
		lines = append(lines, w.logTail(ctx, t)...)
	}
	return redact.Lines(lines)
}

func (w *Watcher) podEvents(ctx context.Context, pod *corev1.Pod) []string {
	list, err := w.cs.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.name=" + pod.Name})
	if err != nil {
		return []string{"events unavailable: " + err.Error()}
	}
	events := slices.DeleteFunc(list.Items, func(e corev1.Event) bool { return e.InvolvedObject.Name != pod.Name })
	slices.SortFunc(events, func(a, b corev1.Event) int { return eventStart(&a).Compare(eventStart(&b)) })
	lines := make([]string, 0, len(events))
	for i := range events {
		e := &events[i]
		lines = append(lines, fmt.Sprintf("%s %s %s: %s", eventStart(e).UTC().Format(time.RFC3339), e.Type, e.Reason, e.Message))
	}
	return lines
}

func (w *Watcher) logTail(ctx context.Context, t trigger) []string {
	n := int64(w.cfg.LogTailLines)
	opts := &corev1.PodLogOptions{Container: t.container, TailLines: &n, Previous: t.previous}
	raw, err := w.cs.CoreV1().Pods(t.pod.Namespace).GetLogs(t.pod.Name, opts).DoRaw(ctx)
	if err != nil {
		return []string{fmt.Sprintf("log of %s unavailable: %v", t.container, err)}
	}
	return append([]string{"log of " + t.container + ":"}, splitLines(raw)...)
}

func splitLines(raw []byte) []string {
	s := strings.TrimRight(string(raw), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
