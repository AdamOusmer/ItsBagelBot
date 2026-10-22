// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package certroll

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

const ns = "app"

func certificate(name, secret string, revision int64, notBefore string) *unstructured.Unstructured {
	obj := map[string]any{
		"apiVersion": "cert-manager.io/v1",
		"kind":       "Certificate",
		"metadata":   map[string]any{"name": name, "namespace": ns},
		"spec":       map[string]any{"secretName": secret},
	}
	if notBefore != "" {
		obj["status"] = map[string]any{"revision": revision, "notBefore": notBefore}
	}
	return &unstructured.Unstructured{Object: obj}
}

func deployment(name string, annotations map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Annotations: annotations}}
}

func dynamicWith(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{certificates: "CertificateList"}, objs...)
}

type fixture struct {
	roller *Roller
	cs     *fake.Clientset
	dyn    *dynamicfake.FakeDynamicClient
}

func newFixture(deps []*appsv1.Deployment, certs ...runtime.Object) fixture {
	cs := fake.NewClientset()
	for _, d := range deps {
		_, _ = cs.AppsV1().Deployments(ns).Create(context.Background(), d, metav1.CreateOptions{})
	}
	dyn := dynamicWith(certs...)
	return fixture{roller: newRoller(dyn, cs, zap.NewNop()), cs: cs, dyn: dyn}
}

func (f fixture) get(t *testing.T, name string) *appsv1.Deployment {
	t.Helper()
	dep, err := f.cs.AppsV1().Deployments(ns).Get(context.Background(), name, metav1.GetOptions{})
	require.NoError(t, err)
	return dep
}

func optIn(secrets string) map[string]string { return map[string]string{AnnotationRestartOn: secrets} }

func TestFirstSightRecordsFingerprintWithoutRolling(t *testing.T) {
	f := newFixture([]*appsv1.Deployment{deployment("web", optIn("web-tls"))},
		certificate("web-tls", "web-tls", 1, "2026-10-01T00:00:00Z"))

	require.NoError(t, f.roller.Reconcile(t.Context(), ns))

	dep := f.get(t, "web")
	assert.NotEmpty(t, dep.Annotations[annotationSeen])
	assert.Empty(t, dep.Spec.Template.Annotations[annotationPodTemplate], "adopting a deployment must not restart it")
}

func TestUnchangedCertificateNeverRolls(t *testing.T) {
	f := newFixture([]*appsv1.Deployment{deployment("web", optIn("web-tls"))},
		certificate("web-tls", "web-tls", 1, "2026-10-01T00:00:00Z"))
	require.NoError(t, f.roller.Reconcile(t.Context(), ns))
	before := f.get(t, "web")

	require.NoError(t, f.roller.Reconcile(t.Context(), ns))

	assert.Equal(t, before.Annotations, f.get(t, "web").Annotations)
	assert.Empty(t, f.get(t, "web").Spec.Template.Annotations)
}

func TestRenewalStampsPodTemplateOnce(t *testing.T) {
	f := newFixture([]*appsv1.Deployment{deployment("web", optIn("web-tls"))},
		certificate("web-tls", "web-tls", 1, "2026-10-01T00:00:00Z"))
	require.NoError(t, f.roller.Reconcile(t.Context(), ns))
	first := f.get(t, "web").Annotations[annotationSeen]

	renewed := certificate("web-tls", "web-tls", 2, "2026-11-01T00:00:00Z")
	_, err := f.dyn.Resource(certificates).Namespace(ns).Update(t.Context(), renewed, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, f.roller.Reconcile(t.Context(), ns))

	dep := f.get(t, "web")
	assert.NotEqual(t, first, dep.Annotations[annotationSeen])
	assert.Equal(t, dep.Annotations[annotationSeen], dep.Spec.Template.Annotations[annotationPodTemplate])

	stamped := dep.Spec.Template.Annotations[annotationPodTemplate]
	require.NoError(t, f.roller.Reconcile(t.Context(), ns))
	assert.Equal(t, stamped, f.get(t, "web").Spec.Template.Annotations[annotationPodTemplate])
}

func TestOnlyOptedInDeploymentsAreTouched(t *testing.T) {
	f := newFixture([]*appsv1.Deployment{deployment("other", nil), deployment("web", optIn("web-tls"))},
		certificate("web-tls", "web-tls", 1, "2026-10-01T00:00:00Z"))

	require.NoError(t, f.roller.Reconcile(t.Context(), ns))

	assert.Empty(t, f.get(t, "other").Annotations)
}

func TestUnissuedCertificateIsNotFingerprinted(t *testing.T) {
	f := newFixture([]*appsv1.Deployment{deployment("web", optIn("web-tls"))}, certificate("web-tls", "web-tls", 0, ""))

	require.NoError(t, f.roller.Reconcile(t.Context(), ns))

	assert.Empty(t, f.get(t, "web").Annotations[annotationSeen])
}

func TestAnySecretRenewalRollsAMultiSecretDeployment(t *testing.T) {
	f := newFixture([]*appsv1.Deployment{deployment("web", optIn("server-tls, client-tls"))},
		certificate("server", "server-tls", 1, "2026-10-01T00:00:00Z"),
		certificate("client", "client-tls", 1, "2026-10-01T00:00:00Z"))
	require.NoError(t, f.roller.Reconcile(t.Context(), ns))

	_, err := f.dyn.Resource(certificates).Namespace(ns).Update(t.Context(),
		certificate("client", "client-tls", 2, "2026-11-01T00:00:00Z"), metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, f.roller.Reconcile(t.Context(), ns))

	assert.NotEmpty(t, f.get(t, "web").Spec.Template.Annotations[annotationPodTemplate])
}

func TestRunRollsAfterRenewalEvent(t *testing.T) {
	f := newFixture([]*appsv1.Deployment{deployment("web", optIn("web-tls"))},
		certificate("web-tls", "web-tls", 1, "2026-10-01T00:00:00Z"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.roller.Run(ctx, []string{ns}, 20*time.Millisecond)
	require.Eventually(t, func() bool { return f.get(t, "web").Annotations[annotationSeen] != "" },
		5*time.Second, 10*time.Millisecond)

	_, err := f.dyn.Resource(certificates).Namespace(ns).Update(ctx,
		certificate("web-tls", "web-tls", 2, "2026-11-01T00:00:00Z"), metav1.UpdateOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() bool { return f.get(t, "web").Spec.Template.Annotations[annotationPodTemplate] != "" },
		5*time.Second, 10*time.Millisecond)
}
