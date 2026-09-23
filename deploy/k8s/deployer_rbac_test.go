// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"reflect"
	"slices"
	"testing"
)

// rbacManifest reads the four RBAC kinds out of deployer.yaml. Roles fill
// Rules, bindings fill RoleRef and Subjects.
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

// rbacKinds groups deployer.yaml's RBAC documents by kind.
func rbacKinds(t *testing.T) map[string][]rbacManifest {
	t.Helper()
	out := map[string][]rbacManifest{}
	for _, manifest := range decodeFile[rbacManifest](t, deployerManifest) {
		out[manifest.Kind] = append(out[manifest.Kind], manifest)
	}
	return out
}

// deniedGrants are what the deployer must never hold. delete: the train only
// adds or changes, and a removal is a human decision. secrets: Doppler owns
// every Secret, and reading one is reading another service's credentials.
// RBAC: a train that can write a RoleBinding, or escalate, bind or
// impersonate, can grant itself anything, so every other limit here would be
// advisory. A wildcard grants all of the above at once.
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

// TestDeployerRBACNeverGrantsDeleteSecretsOrRBAC walks every rule of every
// Role and ClusterRole in deployer.yaml, so a grant added in any of them, or
// a new role, is checked without editing this test.
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

// deniedIn lists each denied value a rule grants, as "field value".
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

// TestDeployerRolesMirrorTheApplierAllowlist: one Role per workload
// namespace, identical in each, whose write grants are the applier's
// allowlist (app/deployer/internal/kube/apply) one for one. RBAC is the outer
// bound and the allowlist the inner one; when they match, a refused object
// fails on the applier's readable message instead of a 403 half way through
// an apply. ops is absent on purpose: the deployer never rolls itself.
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

// allowlist is the applier's kind allowlist as "group/resource", sorted.
// PriorityClass is the cluster-scoped member and lives on the ClusterRole.
var allowlist = sorted(
	"/configmaps", "/services", "apps/daemonsets", "apps/deployments", "batch/cronjobs",
	"keda.sh/scaledobjects", "networking.k8s.io/networkpolicies", "policy/poddisruptionbudgets",
	"traefik.io/ingressroutes", "traefik.io/middlewares",
)

// writableResources lists every "group/resource" a rule set can patch.
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

// targets lists the rule's "group/resource" pairs.
func (r rbacRule) targets() []string {
	var out []string
	for _, group := range r.APIGroups {
		for _, resource := range r.Resources {
			out = append(out, group+"/"+resource)
		}
	}
	return out
}

// TestDeployerClusterRoleIsPriorityClassesAndNodes: the only cluster-scoped
// needs are applying priorityclasses.yaml first on every rollout and
// preflight's node listing. Exact, because a cluster-scoped grant reaches
// every namespace, including the ones the Roles leave out.
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

// TestDeployerBindingsNameOnlyItsServiceAccount: every binding grants to the
// deployer ServiceAccount in ops and nothing else, and binds the roles above
// in exactly the namespaces they live in.
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
