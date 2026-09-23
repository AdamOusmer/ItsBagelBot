// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package apply

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/codec"
)

var (
	scaledObjects = schema.GroupVersionResource{Group: "keda.sh", Version: "v1alpha1", Resource: "scaledobjects"}
	replicas3     = map[string]any{"replicas": int64(3)}
)

// patch is what the fake API server saw for one server-side apply.
type patch struct {
	Ref      string
	Manager  string
	Force    bool
	Replicas bool
}

type cluster struct {
	client  *dynamicfake.FakeDynamicClient
	patches []patch
	failOn  string
}

// newCluster fakes an API server that knows the allowlisted kinds (KEDA
// optional) and answers apply patches the way a real one does for this test:
// a new object gets resourceVersion 1, an existing one keeps its version (a
// no-op apply).
func newCluster(t *testing.T, withKEDA bool, live ...runtime.Object) (*cluster, *Applier) {
	t.Helper()
	var known []schema.GroupKind
	for gk := range allowed {
		if gk != scaledObjectKind || withKEDA {
			known = append(known, gk)
		}
	}
	// The preferred versions let a versionless RESTMapping (the live
	// ScaledObject list) resolve, as discovery's PriorityRESTMapper does.
	gvs := make([]schema.GroupVersion, len(known))
	for i, gk := range known {
		gvs[i] = gk.WithVersion(versionOf(gk)).GroupVersion()
	}
	mapper := meta.NewDefaultRESTMapper(gvs)
	for _, gk := range known {
		mapper.Add(gk.WithVersion(versionOf(gk)), restScope(allowed[gk]))
	}
	c := &cluster{client: dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{scaledObjects: "ScaledObjectList"}, live...)}
	c.client.PrependReactor("patch", "*", c.react)
	return c, newApplier(c.client, mapper)
}

func (c *cluster) react(action k8stesting.Action) (bool, runtime.Object, error) {
	pa := action.(k8stesting.PatchActionImpl)
	o := &unstructured.Unstructured{}
	if err := codec.Unmarshal(pa.GetPatch(), &o.Object); err != nil {
		return true, nil, err
	}
	ref := refString(refOf(o))
	if ref == c.failOn {
		return true, nil, errors.New("admission webhook denied the request")
	}
	_, hasReplicas, _ := unstructured.NestedFieldNoCopy(o.Object, "spec", "replicas")
	c.patches = append(c.patches, patch{Ref: ref, Manager: pa.PatchOptions.FieldManager, Force: *pa.PatchOptions.Force, Replicas: hasReplicas})
	o.SetResourceVersion("1")
	if live, err := c.client.Tracker().Get(pa.GetResource(), pa.GetNamespace(), pa.GetName()); err == nil {
		acc, _ := meta.Accessor(live)
		o.SetResourceVersion(acc.GetResourceVersion())
	}
	return true, o, nil
}

func versionOf(gk schema.GroupKind) string {
	versions := map[string]string{"": "v1", "apps": "v1", "batch": "v1", "policy": "v1",
		"networking.k8s.io": "v1", "scheduling.k8s.io": "v1", "traefik.io": "v1alpha1", "keda.sh": "v1alpha1"}
	return versions[gk.Group]
}

func restScope(sc scope) meta.RESTScope {
	if sc == scopeCluster {
		return meta.RESTScopeRoot
	}
	return meta.RESTScopeNamespace
}

func TestApply(t *testing.T) {
	liveSesameScaler := manifest{apiVersion: "keda.sh/v1alpha1", kind: "ScaledObject", ns: "app", name: "sesame-scaler",
		spec: map[string]any{"scaleTargetRef": map[string]any{"name": "sesame"}}}.obj()
	liveConfig := manifest{apiVersion: "v1", kind: "ConfigMap", ns: "db", name: "backup-k3s-scripts"}.obj()
	liveConfig.SetResourceVersion("7")
	objs := objects(
		manifest{apiVersion: "apps/v1", kind: "Deployment", ns: "app", name: "outgress", spec: replicas3},
		manifest{apiVersion: "keda.sh/v1alpha1", kind: "ScaledObject", ns: "app", name: "outgress",
			spec: map[string]any{"scaleTargetRef": map[string]any{"name": "outgress"}}},
		manifest{apiVersion: "apps/v1", kind: "Deployment", ns: "app", name: "sesame", spec: replicas3},
		manifest{apiVersion: "apps/v1", kind: "Deployment", ns: "db", name: "commands", spec: replicas3},
		manifest{apiVersion: "v1", kind: "ConfigMap", ns: "db", name: "backup-k3s-scripts"},
		manifest{apiVersion: "scheduling.k8s.io/v1", kind: "PriorityClass", name: "bagel-edge"},
	)
	c, a := newCluster(t, true, liveSesameScaler, liveConfig)

	res, err := a.Apply(context.Background(), objs)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	type outcome struct {
		Patches       []patch
		Changed       []string
		Applied       int
		InputReplicas bool
	}
	_, inputReplicas, _ := unstructured.NestedFieldNoCopy(objs[0].Object, "spec", "replicas")
	got := outcome{Patches: c.patches, Changed: refStrings(res.Changed), Applied: len(res.Applied), InputReplicas: inputReplicas}
	want := outcome{
		Patches: []patch{
			{Ref: "PriorityClass bagel-edge", Manager: FieldManager, Force: true},
			{Ref: "Deployment app/outgress", Manager: FieldManager, Force: true},
			{Ref: "ScaledObject app/outgress", Manager: FieldManager, Force: true},
			{Ref: "Deployment app/sesame", Manager: FieldManager, Force: true},
			{Ref: "Deployment db/commands", Manager: FieldManager, Force: true, Replicas: true},
			{Ref: "ConfigMap db/backup-k3s-scripts", Manager: FieldManager, Force: true},
		},
		Changed: []string{"PriorityClass bagel-edge", "Deployment app/outgress", "ScaledObject app/outgress",
			"Deployment app/sesame", "Deployment db/commands"},
		Applied:       6,
		InputReplicas: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("apply outcome\n got %+v\nwant %+v", got, want)
	}
}

// TestApplyWithoutKEDA: a cluster with no ScaledObject CRD has no live
// ScaledObjects, so replicas stay and the apply proceeds.
func TestApplyWithoutKEDA(t *testing.T) {
	c, a := newCluster(t, false)
	objs := objects(manifest{apiVersion: "apps/v1", kind: "Deployment", ns: "db", name: "users", spec: replicas3})
	if _, err := a.Apply(context.Background(), objs); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []patch{{Ref: "Deployment db/users", Manager: FieldManager, Force: true, Replicas: true}}
	if !reflect.DeepEqual(c.patches, want) {
		t.Fatalf("patches = %+v, want %+v", c.patches, want)
	}
}

func TestApplyStopsAtFirstFailure(t *testing.T) {
	c, a := newCluster(t, true)
	c.failOn = "Deployment db/loyalty"
	objs := objects(
		manifest{apiVersion: "apps/v1", kind: "Deployment", ns: "db", name: "commands"},
		manifest{apiVersion: "apps/v1", kind: "Deployment", ns: "db", name: "loyalty"},
		manifest{apiVersion: "apps/v1", kind: "Deployment", ns: "db", name: "modules"},
	)
	res, err := a.Apply(context.Background(), objs)
	f, ok := ports.AsFail(err)
	type outcome struct {
		Code    deploy.FailureCode
		Applied []string
	}
	got := outcome{Applied: refStrings(res.Applied)}
	if ok {
		got.Code = f.Code
	}
	want := outcome{Code: deploy.FailApplyFailed, Applied: []string{"Deployment db/commands"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v (err %v), want %+v", got, err, want)
	}
}

func TestAllowlist(t *testing.T) {
	objs := objects(
		manifest{apiVersion: "apps/v1", kind: "Deployment", ns: "app", name: "gossip"},
		manifest{apiVersion: "v1", kind: "Secret", ns: "app", name: "gossip-env"},
		manifest{apiVersion: "rbac.authorization.k8s.io/v1", kind: "Role", ns: "db", name: "deployer"},
		manifest{apiVersion: "apps/v1", kind: "Deployment", ns: "ops", name: "deployer"},
		manifest{apiVersion: "secrets.doppler.com/v1alpha1", kind: "DopplerSecret", ns: "db", name: "users-env"},
		manifest{apiVersion: "example.com/v1", kind: "Deployment", ns: "app", name: "lookalike"},
		manifest{apiVersion: "scheduling.k8s.io/v1", kind: "PriorityClass", name: "bagel-edge"},
	)
	c, a := newCluster(t, true)
	_, err := a.Apply(context.Background(), objs)
	f, _ := ports.AsFail(err)
	ok, out := Split(objs)
	type outcome struct {
		Refused  bool
		Code     deploy.FailureCode
		LogLines int
		Actions  int
		Accepted []string
		Outside  []string
	}
	got := outcome{Refused: errors.Is(err, ports.ErrRefused), LogLines: len(f.LogTail), Actions: len(c.client.Actions()),
		Code: f.Code, Accepted: refs(ok, nil), Outside: refStrings(out)}
	want := outcome{
		Refused: true, Code: deploy.FailApplyRefused, LogLines: 5, Actions: 0,
		Accepted: []string{"Deployment app/gossip", "PriorityClass bagel-edge"},
		Outside: []string{"Secret app/gossip-env", "Role db/deployer", "Deployment ops/deployer",
			"DopplerSecret db/users-env", "Deployment app/lookalike"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func refStrings(rs []ports.ObjectRef) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, refString(r))
	}
	return out
}
