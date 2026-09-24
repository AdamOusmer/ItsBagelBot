// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watch

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

type Watcher struct {
	cs   kubernetes.Interface
	cfg  ports.Config
	http *http.Client
	repo imageRepo
	now  func() time.Time
}

var _ ports.Watcher = (*Watcher)(nil)

func New(rc *rest.Config, cfg ports.Config, hc *http.Client) (*Watcher, error) {
	cs, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, fmt.Errorf("kube client: %w", err)
	}
	return newWatcher(cs, cfg, hc), nil
}

func newWatcher(cs kubernetes.Interface, cfg ports.Config, hc *http.Client) *Watcher {
	client := *hc
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Watcher{cs: cs, cfg: cfg, http: &client, repo: imageRepo(cfg.ImageRepo), now: time.Now}
}

func (w *Watcher) Reachable(ctx context.Context) error {
	if _, err := w.cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		return fmt.Errorf("kube api: %w", err)
	}
	return nil
}

func (w *Watcher) Settled(ctx context.Context, refs []ports.WorkloadRef) ([]ports.Unsettled, error) {
	var out []ports.Unsettled
	err := w.eachWorkload(ctx, refs, func(ref ports.WorkloadRef, wl *workload) error {
		if wl.unsettled != "" {
			out = append(out, ports.Unsettled{Workload: ref, Reason: wl.unsettled})
		}
		return nil
	})
	return out, err
}

func (w *Watcher) LiveImages(ctx context.Context, refs []ports.WorkloadRef) ([]ports.LiveImage, error) {
	var out []ports.LiveImage
	err := w.eachWorkload(ctx, refs, func(ref ports.WorkloadRef, wl *workload) error {
		for _, c := range podContainers(&wl.template.Spec) {
			out = append(out, ports.LiveImage{Workload: ref, Container: c.Name, Image: c.Image})
		}
		return nil
	})
	return out, err
}

func (w *Watcher) VerifyImageIDs(ctx context.Context, refs []ports.WorkloadRef, pins ports.Pins) ([]ports.Mismatch, error) {
	var out []ports.Mismatch
	err := w.eachWorkload(ctx, refs, func(ref ports.WorkloadRef, wl *workload) error {
		if wl.selector == nil {
			return nil
		}
		pods, err := w.pods(ctx, ref.Namespace, wl.selector)
		for i := range pods {
			out = append(out, w.podMismatches(ref, &pods[i], pins)...)
		}
		return err
	})
	return out, err
}

func (w *Watcher) podMismatches(ref ports.WorkloadRef, pod *corev1.Pod, pins ports.Pins) []ports.Mismatch {
	if pod.DeletionTimestamp != nil {
		return nil
	}
	ids := imageIDs(pod)
	var out []ports.Mismatch
	for _, c := range podContainers(&pod.Spec) {
		want, ok := pins[w.repo.name(c.Image)]
		if !ok || ids[c.Name] == want {
			continue
		}
		out = append(out, ports.Mismatch{Workload: ref, Pod: pod.Name, Container: c.Name, Want: want, Got: ids[c.Name]})
	}
	return out
}

func (w *Watcher) Probe(ctx context.Context, url ports.URL) (int, error) {
	resp, err := w.get(ctx, string(url))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func (w *Watcher) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return w.http.Do(req)
}

type imageRepo string

func (r imageRepo) name(image string) deploy.ImageName {
	rest, ok := strings.CutPrefix(image, string(r)+"/")
	if !ok {
		return ""
	}
	rest, _, _ = strings.Cut(rest, "@")
	rest, _, _ = strings.Cut(rest, ":")
	return deploy.ImageName(rest)
}

func digestOf(imageID string) deploy.Digest {
	i := strings.LastIndex(imageID, "@")
	if i < 0 {
		return ""
	}
	return deploy.Digest(imageID[i+1:])
}

func imageIDs(pod *corev1.Pod) map[string]deploy.Digest {
	ids := map[string]deploy.Digest{}
	for _, s := range containerStatuses(pod) {
		ids[s.Name] = digestOf(s.ImageID)
	}
	return ids
}

func podContainers(spec *corev1.PodSpec) []corev1.Container {
	return slices.Concat(spec.Containers, spec.InitContainers)
}

func containerStatuses(pod *corev1.Pod) []corev1.ContainerStatus {
	return slices.Concat(pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses)
}
