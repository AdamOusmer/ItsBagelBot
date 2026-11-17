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
	for _, manifest := range decodeFile[rbacManifest](t, reloaderManifest) {
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
		[]string{"roles", "rolebindings", "clusterroles", "clusterrolebindings", "serviceaccounts/token", "*"}},
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

func TestReloaderRBACNeverGrantsCreateDeleteOrRBAC(t *testing.T) {
	kinds := rbacKinds(t)
	roles := slices.Concat(kinds["Role"], kinds["ClusterRole"])
	require.NotEmpty(t, roles, "reloader.yaml declares no roles; the decode or the kinds are wrong")

	var granted []string
	for _, role := range roles {
		for _, rule := range role.Rules {
			for _, value := range rule.deniedGrants() {
				granted = append(granted, role.Kind+" "+role.Metadata.Namespace+"/"+role.Metadata.Name+" "+value)
			}
		}
	}

	assert.Empty(t, granted, "reloader RBAC grants what it must never hold")
}

func TestReloaderOnlyReadsAppSecretsAndRollsAppDeployments(t *testing.T) {
	kinds := rbacKinds(t)
	require.Len(t, kinds["Role"], 1)
	role := kinds["Role"][0]

	assert.Empty(t, kinds["ClusterRole"], "rolling the consoles needs no cluster-wide grant")
	assert.Equal(t, "app/reloader", role.Metadata.Namespace+"/"+role.Metadata.Name)
	assert.Equal(t, []rbacRule{
		{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"get", "list", "update", "patch"}},
		{APIGroups: []string{"apps"}, Resources: []string{"daemonsets", "statefulsets"}, Verbs: []string{"get", "list"}},
	}, role.Rules)
}

func TestReloaderBindingsNameOnlyItsServiceAccount(t *testing.T) {
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
		"RoleBinding app/reloader": {
			RoleRef:  rbacRoleRef{Kind: "Role", Name: "reloader"},
			Subjects: []rbacSubject{{Kind: "ServiceAccount", Name: "reloader", Namespace: "ops"}},
		},
	}, got)
}
