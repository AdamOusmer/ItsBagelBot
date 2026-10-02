// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package apply

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
		want   []ports.ObjectRef
	}{
		{
			name:   "real manifests pass",
			ref:    outgressRef,
			mutate: func(*unstructured.Unstructured) {},
			want:   []ports.ObjectRef{},
		},
		{
			name: "required anti-affinity with maxSurge 1",
			ref:  notificationsRef,
			mutate: func(o *unstructured.Unstructured) {
				_ = unstructured.SetNestedField(o.Object, int64(1), "spec", "strategy", "rollingUpdate", "maxSurge")
			},
			want: []ports.ObjectRef{notificationsRef},
		},
		{
			name: "maxSurge 0% does not surge",
			ref:  notificationsRef,
			mutate: func(o *unstructured.Unstructured) {
				_ = unstructured.SetNestedField(o.Object, "0%", "spec", "strategy", "rollingUpdate", "maxSurge")
			},
			want: []ports.ObjectRef{},
		},
		{
			name: "Recreate never surges",
			ref:  notificationsRef,
			mutate: func(o *unstructured.Unstructured) {
				_ = unstructured.SetNestedMap(o.Object, map[string]any{"type": "Recreate"}, "spec", "strategy")
			},
			want: []ports.ObjectRef{},
		},
		{
			name:   "no strategy defaults to 25% surge",
			ref:    notificationsRef,
			mutate: func(o *unstructured.Unstructured) { unstructured.RemoveNestedField(o.Object, "spec", "strategy") },
			want:   []ports.ObjectRef{notificationsRef},
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
			want: []ports.ObjectRef{consoleAdminRef},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objs := clone(base)
			tc.mutate(find(t, objs, tc.ref))
			got := []ports.ObjectRef{}
			for _, f := range new(Applier).Lint(objs) {
				got = append(got, f.Object)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
