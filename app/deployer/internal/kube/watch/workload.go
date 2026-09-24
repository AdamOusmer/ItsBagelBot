// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"ItsBagelBot/app/deployer/internal/ports"
)

const (
	kindDeployment = "Deployment"
	kindDaemonSet  = "DaemonSet"
	kindCronJob    = "CronJob"
)

type workload struct {
	template  corev1.PodTemplateSpec
	selector  *metav1.LabelSelector
	unsettled string
}

type fetchFunc func(ctx context.Context, cs kubernetes.Interface, ref ports.WorkloadRef) (*workload, error)

var fetchers = map[string]fetchFunc{
	kindDeployment: fetchDeployment,
	kindDaemonSet:  fetchDaemonSet,
	kindCronJob:    fetchCronJob,
}

func (w *Watcher) eachWorkload(ctx context.Context, refs []ports.WorkloadRef, fn func(ports.WorkloadRef, *workload) error) error {
	for _, ref := range refs {
		wl, err := w.workload(ctx, ref)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if err := fn(ref, wl); err != nil {
			return err
		}
	}
	return nil
}

func (w *Watcher) workload(ctx context.Context, ref ports.WorkloadRef) (*workload, error) {
	fetch, ok := fetchers[ref.Kind]
	if !ok {
		return nil, fmt.Errorf("%w: workload kind %q", ports.ErrInvalid, ref.Kind)
	}
	wl, err := fetch(ctx, w.cs, ref)
	if apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("%w: %s %s/%s", ports.ErrNotFound, ref.Kind, ref.Namespace, ref.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("get %s %s/%s: %w", ref.Kind, ref.Namespace, ref.Name, err)
	}
	return wl, nil
}

func fetchDeployment(ctx context.Context, cs kubernetes.Interface, ref ports.WorkloadRef) (*workload, error) {
	d, err := cs.AppsV1().Deployments(string(ref.Namespace)).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &workload{template: d.Spec.Template, selector: d.Spec.Selector, unsettled: deploymentUnsettled(d)}, nil
}

func fetchDaemonSet(ctx context.Context, cs kubernetes.Interface, ref ports.WorkloadRef) (*workload, error) {
	ds, err := cs.AppsV1().DaemonSets(string(ref.Namespace)).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	s := ds.Status
	c := counts{current: s.CurrentNumberScheduled, updated: s.UpdatedNumberScheduled, ready: s.NumberReady, available: s.NumberAvailable}
	return &workload{template: ds.Spec.Template, selector: ds.Spec.Selector, unsettled: unsettled(ds.Generation, s.ObservedGeneration, s.DesiredNumberScheduled, c)}, nil
}

func fetchCronJob(ctx context.Context, cs kubernetes.Interface, ref ports.WorkloadRef) (*workload, error) {
	cj, err := cs.BatchV1().CronJobs(string(ref.Namespace)).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &workload{template: cj.Spec.JobTemplate.Spec.Template}, nil
}

type counts struct{ current, updated, ready, available int32 }

func unsettled(gen, observed int64, want int32, c counts) string {
	if observed < gen {
		return fmt.Sprintf("generation %d not observed yet (at %d)", gen, observed)
	}
	switch {
	case c.updated != want:
		return fmt.Sprintf("%d/%d updated", c.updated, want)
	case c.ready != want:
		return fmt.Sprintf("%d/%d ready", c.ready, want)
	case c.available != want:
		return fmt.Sprintf("%d/%d available", c.available, want)
	case c.current != want:
		return fmt.Sprintf("%d pods for %d replicas", c.current, want)
	}
	return ""
}

func deploymentUnsettled(d *appsv1.Deployment) string {
	s := d.Status
	c := counts{current: s.Replicas, updated: s.UpdatedReplicas, ready: s.ReadyReplicas, available: s.AvailableReplicas}
	return unsettled(d.Generation, s.ObservedGeneration, desired(d.Spec.Replicas), c)
}

func desired(replicas *int32) int32 {
	if replicas == nil {
		return 1
	}
	return *replicas
}

func (w *Watcher) pods(ctx context.Context, ns ports.Namespace, sel *metav1.LabelSelector) ([]corev1.Pod, error) {
	s, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		return nil, fmt.Errorf("selector: %w", err)
	}
	return w.listPods(ctx, ns, s.String())
}

func (w *Watcher) listPods(ctx context.Context, ns ports.Namespace, selector string) ([]corev1.Pod, error) {
	list, err := w.cs.CoreV1().Pods(string(ns)).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list pods in %s: %w", ns, err)
	}
	slices.SortFunc(list.Items, func(a, b corev1.Pod) int { return cmp.Compare(a.Name, b.Name) })
	return list.Items, nil
}
