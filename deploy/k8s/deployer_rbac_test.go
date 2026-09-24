// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"reflect"
	"slices"
	"testing"
)

type rbacManifest struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
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

func TestDeployerRBACNeverGrantsDeleteSecretsOrRBAC(t *testing.T) {
	kinds := rbacKinds(t)
	roles := slices.Concat(kinds["Role"], kinds["ClusterRole"])
	if len(roles) == 0 {
		t.Fatal("deployer.yaml declares no roles; the decode or the kinds are wrong")
	}
	var granted []string
	for _, role := range roles {
		for _, rule := range role.Rules {
			for _, value := range deniedIn(rule) {
				granted = append(granted, role.Kind+" "+role.Metadata.Namespace+"/"+role.Metadata.Name+" "+value)
			}
		}
	}
	if len(granted) > 0 {
		t.Fatalf("deployer RBAC grants what it must never hold: %v", granted)
	}
}

func deniedIn(rule rbacRule) []string {
	var out []string
	for _, grant := range deniedGrants {
		for _, value := range grant.of(rule) {
			if slices.Contains(grant.denied, value) {
				out = append(out, grant.field+" "+value)
			}
		}
	}
	return out
}

func TestDeployerRolesMirrorTheApplierAllowlist(t *testing.T) {
	byNamespace := map[string][]rbacRule{}
	for _, role := range rbacKinds(t)["Role"] {
		byNamespace[role.Metadata.Namespace] = role.Rules
	}
	want := map[string][]string{"app": allowlist, "db": allowlist, "messaging": allowlist}
	got := map[string][]string{}
	for namespace, rules := range byNamespace {
		got[namespace] = writableResources(rules)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deployer Role write grants by namespace:\n got %v\nwant %v", got, want)
	}
	if !reflect.DeepEqual(byNamespace["db"], byNamespace["app"]) || !reflect.DeepEqual(byNamespace["messaging"], byNamespace["app"]) {
		t.Fatal("deployer Roles differ between namespaces; keep app, db and messaging identical")
	}
}

var allowlist = sorted(
	"/configmaps", "/services", "apps/daemonsets", "apps/deployments", "batch/cronjobs",
	"keda.sh/scaledobjects", "networking.k8s.io/networkpolicies", "policy/poddisruptionbudgets",
	"traefik.io/ingressroutes", "traefik.io/middlewares",
)

func writableResources(rules []rbacRule) []string {
	var out []string
	for _, rule := range rules {
		if slices.Contains(rule.Verbs, "patch") {
			out = append(out, rule.targets()...)
		}
	}
	slices.Sort(out)
	return out
}

func (r rbacRule) targets() []string {
	var out []string
	for _, group := range r.APIGroups {
		for _, resource := range r.Resources {
			out = append(out, group+"/"+resource)
		}
	}
	return out
}

func TestDeployerClusterRoleIsPriorityClassesAndNodes(t *testing.T) {
	clusterRoles := rbacKinds(t)["ClusterRole"]
	want := []rbacRule{
		{APIGroups: []string{"scheduling.k8s.io"}, Resources: []string{"priorityclasses"}, Verbs: []string{"get", "list", "create", "patch"}},
		{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get", "list"}},
	}
	if len(clusterRoles) != 1 || !reflect.DeepEqual(clusterRoles[0].Rules, want) {
		t.Fatalf("deployer ClusterRoles = %+v, want one bagel-deployer with %+v", clusterRoles, want)
	}
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
	want := map[string]binding{
		"RoleBinding app/deployer":           role,
		"RoleBinding db/deployer":            role,
		"RoleBinding messaging/deployer":     role,
		"ClusterRoleBinding /bagel-deployer": {RoleRef: rbacRoleRef{Kind: "ClusterRole", Name: "bagel-deployer"}, Subjects: deployer},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deployer bindings:\n got %+v\nwant %+v", got, want)
	}
}
