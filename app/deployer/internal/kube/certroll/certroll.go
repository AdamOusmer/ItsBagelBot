// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package certroll

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"ItsBagelBot/pkg/codec"
)

const (
	// Comma-separated TLS secret names; renewing any of them rolls the Deployment.
	AnnotationRestartOn = "itsbagelbot.com/restart-on-tls-secret"

	annotationSeen        = "itsbagelbot.com/tls-fingerprint-seen"
	annotationPodTemplate = "itsbagelbot.com/tls-fingerprint"
)

var certificates = schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}

type Roller struct {
	dyn dynamic.Interface
	cs  kubernetes.Interface
	log *zap.Logger
}

func New(rc *rest.Config, log *zap.Logger) (*Roller, error) {
	dyn, err := dynamic.NewForConfig(rc)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	cs, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, fmt.Errorf("kube client: %w", err)
	}
	return newRoller(dyn, cs, log), nil
}

func newRoller(dyn dynamic.Interface, cs kubernetes.Interface, log *zap.Logger) *Roller {
	return &Roller{dyn: dyn, cs: cs, log: log}
}

// Reads Certificate status, never Secrets: the deployer holds no Secret access and must not gain it.
func (r *Roller) Reconcile(ctx context.Context, namespace string) error {
	issued, err := r.fingerprints(ctx, namespace)
	if err != nil {
		return err
	}
	deployments, err := r.cs.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list deployments in %s: %w", namespace, err)
	}
	var errs []error
	for i := range deployments.Items {
		errs = append(errs, r.reconcileDeployment(ctx, &deployments.Items[i], issued))
	}
	return errors.Join(errs...)
}

func (r *Roller) fingerprints(ctx context.Context, namespace string) (map[string]string, error) {
	list, err := r.dyn.Resource(certificates).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list certificates in %s: %w", namespace, err)
	}
	out := map[string]string{}
	for i := range list.Items {
		secret, _, _ := unstructured.NestedString(list.Items[i].Object, "spec", "secretName")
		if fp, ok := issuance(&list.Items[i]); ok && secret != "" {
			out[secret] = fp
		}
	}
	return out, nil
}

func issuance(cert *unstructured.Unstructured) (string, bool) {
	revision, found, _ := unstructured.NestedInt64(cert.Object, "status", "revision")
	notBefore, _, _ := unstructured.NestedString(cert.Object, "status", "notBefore")
	if !found || notBefore == "" {
		return "", false
	}
	return fmt.Sprintf("%d@%s", revision, notBefore), true
}

func (r *Roller) reconcileDeployment(ctx context.Context, dep *appsv1.Deployment, issued map[string]string) error {
	secrets := watchedSecrets(dep)
	if len(secrets) == 0 {
		return nil
	}
	fp, ok := combine(secrets, issued)
	if !ok {
		r.log.Warn("tls secret has no issued certificate yet, not rolling",
			zap.String("deployment", dep.Name), zap.Strings("secrets", secrets))
		return nil
	}
	seen := dep.Annotations[annotationSeen]
	if seen == fp {
		return nil
	}
	if err := r.mark(ctx, dep, fp, seen != ""); err != nil {
		return fmt.Errorf("roll %s/%s: %w", dep.Namespace, dep.Name, err)
	}
	return nil
}

func watchedSecrets(dep *appsv1.Deployment) []string {
	var out []string
	for name := range strings.SplitSeq(dep.Annotations[AnnotationRestartOn], ",") {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func combine(secrets []string, issued map[string]string) (string, bool) {
	h := sha256.New()
	for _, name := range secrets {
		fp, ok := issued[name]
		if !ok {
			return "", false
		}
		fmt.Fprintf(h, "%s=%s\n", name, fp)
	}
	return hex.EncodeToString(h.Sum(nil))[:16], true
}

// First sight only records the fingerprint: stamping the pod template would roll a healthy Deployment for nothing.
func (r *Roller) mark(ctx context.Context, dep *appsv1.Deployment, fp string, roll bool) error {
	patch := map[string]any{"metadata": map[string]any{"annotations": map[string]string{annotationSeen: fp}}}
	if roll {
		patch["spec"] = map[string]any{"template": map[string]any{"metadata": map[string]any{
			"annotations": map[string]string{annotationPodTemplate: fp},
		}}}
	}
	body, err := codec.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = r.cs.AppsV1().Deployments(dep.Namespace).Patch(ctx, dep.Name, types.StrategicMergePatchType, body, metav1.PatchOptions{})
	if err == nil && roll {
		r.log.Info("tls certificate renewed, rolling deployment",
			zap.String("namespace", dep.Namespace), zap.String("deployment", dep.Name), zap.String("fingerprint", fp))
	}
	return err
}
