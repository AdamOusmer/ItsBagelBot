// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rbacManifest struct {
	Kind     string        `yaml:"kind"`
	Metadata objectMeta    `yaml:"metadata"`
	Rules    []rbacRule    `yaml:"rules"`
	RoleRef  rbacRoleRef   `yaml:"roleRef"`
	Subjects []rbacSubject `yaml:"subjects"`
}

type rbacRule struct {
	APIGroups     []string `yaml:"apiGroups"`
	Resources     []string `yaml:"resources"`
	Verbs         []string `yaml:"verbs"`
	ResourceNames []string `yaml:"resourceNames"`
}

type rbacRoleRef struct {
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
}

type rbacSubject struct {
	Kind      string `yaml:"kind"`
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

func rbacKinds(t *testing.T) map[string][]rbacManifest {
	t.Helper()
	out := map[string][]rbacManifest{}
	for _, manifest := range decodeFile[rbacManifest](t, clusterOpsManifest) {
		out[manifest.Kind] = append(out[manifest.Kind], manifest)
	}
	return out
}

var deniedGrants = []struct {
	field  string
	of     func(rbacRule) []string
	denied []string
}{
	{"verb", func(r rbacRule) []string { return r.Verbs },
		[]string{"create", "delete", "deletecollection", "escalate", "bind", "impersonate", "*"}},
	{"resource", func(r rbacRule) []string { return r.Resources },
		[]string{"secrets", "roles", "rolebindings", "clusterroles", "clusterrolebindings", "serviceaccounts/token", "*"}},
	{"apiGroup", func(r rbacRule) []string { return r.APIGroups },
		[]string{"rbac.authorization.k8s.io", "*"}},
}

func (r rbacRule) deniedGrants() []string {
	var out []string
	for _, grant := range deniedGrants {
		for _, value := range grant.of(r) {
			if slices.Contains(grant.denied, value) {
				out = append(out, grant.field+" "+value)
			}
		}
	}
	return out
}

func TestClusterOpsRBACNeverGrantsCreateDeleteSecretsOrRBAC(t *testing.T) {
	kinds := rbacKinds(t)
	roles := slices.Concat(kinds["Role"], kinds["ClusterRole"])
	require.NotEmpty(t, roles, "clusterops.yaml declares no roles; the decode or the kinds are wrong")

	var granted []string
	for _, role := range roles {
		for _, rule := range role.Rules {
			for _, value := range rule.deniedGrants() {
				granted = append(granted, role.Kind+" "+role.Metadata.Namespace+"/"+role.Metadata.Name+" "+value)
			}
		}
	}

	assert.Empty(t, granted, "clusterops RBAC grants what it must never hold")
}

func TestClusterOpsOnlyRollsAppDeployments(t *testing.T) {
	kinds := rbacKinds(t)
	require.Len(t, kinds["Role"], 1)
	role := kinds["Role"][0]

	assert.Empty(t, kinds["ClusterRole"], "the cert roller and claims sync need no cluster-wide grant")
	assert.Equal(t, "app/clusterops", role.Metadata.Namespace+"/"+role.Metadata.Name)
	assert.Equal(t, []rbacRule{
		{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"get", "list", "watch", "patch"}},
		{APIGroups: []string{"cert-manager.io"}, Resources: []string{"certificates"}, Verbs: []string{"get", "list", "watch"}},
	}, role.Rules)
}

func TestClusterOpsBindingsNameOnlyItsServiceAccount(t *testing.T) {
	type binding struct {
		RoleRef  rbacRoleRef
		Subjects []rbacSubject
	}
	kinds := rbacKinds(t)
	got := map[string]binding{}
	for _, b := range slices.Concat(kinds["RoleBinding"], kinds["ClusterRoleBinding"]) {
		got[b.Kind+" "+b.Metadata.Namespace+"/"+b.Metadata.Name] = binding{RoleRef: b.RoleRef, Subjects: b.Subjects}
	}

	assert.Equal(t, map[string]binding{
		"RoleBinding app/clusterops": {
			RoleRef:  rbacRoleRef{Kind: "Role", Name: "clusterops"},
			Subjects: []rbacSubject{{Kind: "ServiceAccount", Name: "clusterops", Namespace: "ops"}},
		},
	}, got)
}
