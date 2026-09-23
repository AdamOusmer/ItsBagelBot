// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package apply

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"ItsBagelBot/app/deployer/internal/ports"
)

var (
	outgressRef      = ports.ObjectRef{Kind: "Deployment", Namespace: "app", Name: "outgress"}
	notificationsRef = ports.ObjectRef{Kind: "Deployment", Namespace: "db", Name: "notifications"}
	consoleAdminRef  = ports.ObjectRef{Kind: "Deployment", Namespace: "app", Name: "console-admin"}
)

func TestLint(t *testing.T) {
	base := testdataObjects(t)
	cases := []struct {
		name   string
		ref    ports.ObjectRef
		mutate func(o *unstructured.Unstructured)
		want   []string
	}{
		{
			name:   "real manifests pass",
			ref:    outgressRef,
			mutate: func(*unstructured.Unstructured) {},
			want:   []string{},
		},
		{
			name: "required anti-affinity with maxSurge 1",
			ref:  notificationsRef,
			mutate: func(o *unstructured.Unstructured) {
				_ = unstructured.SetNestedField(o.Object, int64(1), "spec", "strategy", "rollingUpdate", "maxSurge")
			},
			want: []string{"Deployment db/notifications"},
		},
		{
			name: "maxSurge 0% does not surge",
			ref:  notificationsRef,
			mutate: func(o *unstructured.Unstructured) {
				_ = unstructured.SetNestedField(o.Object, "0%", "spec", "strategy", "rollingUpdate", "maxSurge")
			},
			want: []string{},
		},
		{
			name: "Recreate never surges",
			ref:  notificationsRef,
			mutate: func(o *unstructured.Unstructured) {
				_ = unstructured.SetNestedMap(o.Object, map[string]any{"type": "Recreate"}, "spec", "strategy")
			},
			want: []string{},
		},
		{
			name:   "no strategy defaults to 25% surge",
			ref:    notificationsRef,
			mutate: func(o *unstructured.Unstructured) { unstructured.RemoveNestedField(o.Object, "spec", "strategy") },
			want:   []string{"Deployment db/notifications"},
		},
		{
			name: "DoNotSchedule spread not scoped to the ReplicaSet",
			ref:  consoleAdminRef,
			mutate: func(o *unstructured.Unstructured) {
				spread, _, _ := unstructured.NestedSlice(o.Object, "spec", "template", "spec", "topologySpreadConstraints")
				for _, c := range spread {
					delete(c.(map[string]any), "matchLabelKeys")
				}
				_ = unstructured.SetNestedSlice(o.Object, spread, "spec", "template", "spec", "topologySpreadConstraints")
			},
			want: []string{"Deployment app/console-admin"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objs := clone(base)
			tc.mutate(find(t, objs, tc.ref))
			got := []string{}
			for _, f := range lint(objs) {
				got = append(got, refString(f.Object))
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("findings = %q, want %q", got, tc.want)
			}
		})
	}
}
