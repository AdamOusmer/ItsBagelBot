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
	for _, manifest := range decodeFile[rbacManifest](t, deployerManifest) {
		out[manifest.Kind] = append(out[manifest.Kind], manifest)
	}
	return out
}

var allowlist = sorted(
	"/configmaps", "/services", "apps/daemonsets", "apps/deployments", "batch/cronjobs",
	"keda.sh/scaledobjects", "networking.k8s.io/networkpolicies", "policy/poddisruptionbudgets",
	"traefik.io/ingressroutes", "traefik.io/middlewares",
)

var deniedGrants = []struct {
	field  string
	of     func(rbacRule) []string
	denied []string
}{
	{"verb", func(r rbacRule) []string { return r.Verbs },
		[]string{"delete", "deletecollection", "escalate", "bind", "impersonate", "*"}},
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

func (r rbacRule) patchTargets() []string {
	var out []string
	if slices.Contains(r.Verbs, "patch") {
		for _, group := range r.APIGroups {
			for _, resource := range r.Resources {
				out = append(out, group+"/"+resource)
			}
		}
	}
	return out
}

func namespaceRules(t *testing.T) map[string][]rbacRule {
	t.Helper()
	out := map[string][]rbacRule{}
	for _, role := range rbacKinds(t)["Role"] {
		out[role.Metadata.Namespace] = append(out[role.Metadata.Namespace], role.Rules...)
	}
	return out
}

func TestDeployerRBACNeverGrantsDeleteSecretsOrRBAC(t *testing.T) {
	kinds := rbacKinds(t)
	roles := slices.Concat(kinds["Role"], kinds["ClusterRole"])
	require.NotEmpty(t, roles, "deployer.yaml declares no roles; the decode or the kinds are wrong")

	var granted []string
	for _, role := range roles {
		for _, rule := range role.Rules {
			for _, value := range rule.deniedGrants() {
				granted = append(granted, role.Kind+" "+role.Metadata.Namespace+"/"+role.Metadata.Name+" "+value)
			}
		}
	}

	assert.Empty(t, granted, "deployer RBAC grants what it must never hold")
}

func TestDeployerRolesMirrorTheApplierAllowlist(t *testing.T) {
	rules := namespaceRules(t)
	got := map[string][]string{}
	for namespace, namespaceRules := range rules {
		var targets []string
		for _, rule := range namespaceRules {
			targets = append(targets, rule.patchTargets()...)
		}
		got[namespace] = sorted(targets...)
	}

	assert.Equal(t, map[string][]string{"app": allowlist, "db": allowlist, "messaging": allowlist, "ops": {"apps/deployments"}}, got)
	assert.Equal(t, rules["app"], rules["db"], "keep the app and db deployer Roles identical")
	assert.Equal(t, rules["app"], rules["messaging"], "keep the app and messaging deployer Roles identical")
}

func TestDeployerPatchesOnlyItselfInOps(t *testing.T) {
	writes := func(verb string) bool { return !slices.Contains([]string{"get", "list", "watch"}, verb) }
	grants := slices.DeleteFunc(namespaceRules(t)["ops"], func(r rbacRule) bool { return !slices.ContainsFunc(r.Verbs, writes) })

	assert.Equal(t, []rbacRule{{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"patch"}, ResourceNames: []string{"deployer"}}}, grants)
}

func TestDeployerClusterRoleIsPriorityClassesAndNodes(t *testing.T) {
	clusterRoles := rbacKinds(t)["ClusterRole"]

	require.Len(t, clusterRoles, 1)
	assert.Equal(t, []rbacRule{
		{APIGroups: []string{"scheduling.k8s.io"}, Resources: []string{"priorityclasses"}, Verbs: []string{"get", "list", "create", "patch"}},
		{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get", "list"}},
	}, clusterRoles[0].Rules)
}

func TestDeployerBindingsNameOnlyItsServiceAccount(t *testing.T) {
	type binding struct {
		RoleRef  rbacRoleRef
		Subjects []rbacSubject
	}
	kinds := rbacKinds(t)
	got := map[string]binding{}
	for _, b := range slices.Concat(kinds["RoleBinding"], kinds["ClusterRoleBinding"]) {
		got[b.Kind+" "+b.Metadata.Namespace+"/"+b.Metadata.Name] = binding{RoleRef: b.RoleRef, Subjects: b.Subjects}
	}
	deployer := []rbacSubject{{Kind: "ServiceAccount", Name: "deployer", Namespace: "ops"}}
	role := binding{RoleRef: rbacRoleRef{Kind: "Role", Name: "deployer"}, Subjects: deployer}

	assert.Equal(t, map[string]binding{
		"RoleBinding app/deployer":           role,
		"RoleBinding db/deployer":            role,
		"RoleBinding messaging/deployer":     role,
		"RoleBinding ops/deployer":           role,
		"ClusterRoleBinding /bagel-deployer": {RoleRef: rbacRoleRef{Kind: "ClusterRole", Name: "bagel-deployer"}, Subjects: deployer},
	}, got)
}
