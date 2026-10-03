// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	appPolicies      = "network-policies.yaml"
	dbPolicies       = "../db/network-policies.yaml"
	deployerManifest = "deployer.yaml"
)

type networkPolicyManifest struct {
	Kind     string     `yaml:"kind"`
	Metadata objectMeta `yaml:"metadata"`
	Spec     struct {
		PodSelector labelSelector `yaml:"podSelector"`
		PolicyTypes []string      `yaml:"policyTypes"`
		Ingress     []policyRule  `yaml:"ingress"`
		Egress      []policyRule  `yaml:"egress"`
	} `yaml:"spec"`
}

type labelSelector struct {
	MatchLabels      map[string]string `yaml:"matchLabels"`
	MatchExpressions []struct {
		Key    string   `yaml:"key"`
		Values []string `yaml:"values"`
	} `yaml:"matchExpressions"`
}

type policyRule struct {
	To    []policyPeer `yaml:"to"`
	From  []policyPeer `yaml:"from"`
	Ports []policyPort `yaml:"ports"`
}

type policyPeer struct {
	NamespaceSelector *labelSelector `yaml:"namespaceSelector"`
	PodSelector       *labelSelector `yaml:"podSelector"`
	IPBlock           *struct {
		CIDR string `yaml:"cidr"`
	} `yaml:"ipBlock"`
}

type policyPort struct {
	Port     int    `yaml:"port"`
	Protocol string `yaml:"protocol"`
}

func loadNetworkPolicies(t *testing.T, path string) map[string]networkPolicyManifest {
	t.Helper()
	policies := make(map[string]networkPolicyManifest)
	for _, manifest := range decodeFile[networkPolicyManifest](t, path) {
		if manifest.Kind == "NetworkPolicy" {
			policies[manifest.Metadata.Name] = manifest
		}
	}
	return policies
}

func requirePolicy(t *testing.T, policies map[string]networkPolicyManifest, name string) networkPolicyManifest {
	t.Helper()
	policy, ok := policies[name]
	require.True(t, ok, "%s policy is missing", name)
	return policy
}

func (p networkPolicyManifest) selectedApps(t *testing.T) []string {
	t.Helper()
	for _, expression := range p.Spec.PodSelector.MatchExpressions {
		if expression.Key == "app" {
			return sorted(slices.Clone(expression.Values)...)
		}
	}
	require.Fail(t, "policy has no app selector", p.Metadata.Name)
	return nil
}

func (p networkPolicyManifest) grantsEgressPort(target int) bool {
	return slices.ContainsFunc(p.Spec.Egress, func(rule policyRule) bool {
		return slices.ContainsFunc(rule.Ports, func(port policyPort) bool { return port.Port == target })
	})
}

func (s *labelSelector) render() string {
	if s == nil {
		return "any"
	}
	terms := make([]string, 0, len(s.MatchLabels)+len(s.MatchExpressions))
	for key, value := range s.MatchLabels {
		terms = append(terms, key+"="+value)
	}
	for _, expression := range s.MatchExpressions {
		terms = append(terms, expression.Key+" in "+strings.Join(expression.Values, ","))
	}
	slices.Sort(terms)
	return strings.Join(terms, ";")
}

func ruleSummary(rules []policyRule) []string {
	var out []string
	for _, rule := range rules {
		peers := slices.Concat(rule.To, rule.From)
		if len(peers) == 0 {
			peers = []policyPeer{{}}
		}
		ports := make([]string, len(rule.Ports))
		for i, port := range rule.Ports {
			ports[i] = fmt.Sprintf("%d/%s", port.Port, cmp.Or(port.Protocol, "TCP"))
		}
		for _, peer := range peers {
			out = append(out, peer.NamespaceSelector.render()+" | "+peer.PodSelector.render()+" | "+strings.Join(ports, ","))
		}
	}
	return out
}

func TestLockedNamespacesDenyByDefault(t *testing.T) {
	for _, namespace := range []string{"cache", "cert-manager", "keda", "networking", "observability", "tailscale"} {
		t.Run(namespace, func(t *testing.T) {
			policy := requirePolicy(t, loadNetworkPolicies(t, "../"+namespace+"/network-policies.yaml"), "default-deny-"+namespace)

			assert.Empty(t, policy.Spec.PodSelector.render(), "default-deny-%s must select every pod", namespace)
			assert.Equal(t, sorted("Egress", "Ingress"), sorted(policy.Spec.PolicyTypes...))
		})
	}
}

func TestOperatorsPoliciesCoverBothDirections(t *testing.T) {
	policies := loadNetworkPolicies(t, "../operators/network-policies.yaml")
	for _, name := range []string{"doppler-operator", "cloudflared"} {
		assert.Equal(t, sorted("Egress", "Ingress"), sorted(requirePolicy(t, policies, name).Spec.PolicyTypes...), name)
	}
}

func TestDefaultPolicyHasNoBlanketExternalEgress(t *testing.T) {
	base := requirePolicy(t, loadNetworkPolicies(t, appPolicies), "default-deny-apps")

	assert.Contains(t, base.selectedApps(t), "notifications-cleanup", "notifications cleanup job escaped the default-deny policy")
	assert.False(t, base.grantsEgressPort(443), "default policy grants blanket external port 443")
	assert.False(t, base.grantsEgressPort(3306), "default policy grants blanket external port 3306")
}

func TestAppEgressAllowlists(t *testing.T) {
	tests := []struct {
		name      string
		policy    string
		wantApps  []string
		wantCIDRs []string
	}{
		{
			name: "TestPublicHTTPSEgressAllowlist", policy: "allow-public-https",
			wantApps: sorted("commands", "console-admin", "console-dashboard", "discord-ingress", "discord-outgress", "gossip", "loyalty", "modules",
				"notifications", "outgress", "projector", "sesame", "transactions", "twitch-ingress", "users"),
		},
		{
			name: "TestHeatWaveEgressAllowlist", policy: "allow-heatwave",
			wantApps:  sorted("commands", "console-admin", "loyalty", "modules", "notifications", "transactions", "users"),
			wantCIDRs: sorted("10.0.0.0/16", "204.216.107.73/32"),
		},
	}
	policies := loadNetworkPolicies(t, appPolicies)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := requirePolicy(t, policies, tc.policy)

			assert.Equal(t, tc.wantApps, policy.selectedApps(t))
			if tc.wantCIDRs == nil {
				return
			}
			require.Len(t, policy.Spec.Egress, 1, "%s must have exactly one egress rule", tc.policy)
			var cidrs []string
			for _, to := range policy.Spec.Egress[0].To {
				require.NotNil(t, to.IPBlock, "%s egress destinations must all be ipBlocks", tc.policy)
				cidrs = append(cidrs, to.IPBlock.CIDR)
			}
			assert.Equal(t, tc.wantCIDRs, sorted(cidrs...))
		})
	}
}

func TestDBNamespaceCoversEveryDataService(t *testing.T) {
	policies := loadNetworkPolicies(t, dbPolicies)
	want := map[string][]string{
		"default-deny-db": sorted("backup-k3s", "backup-mysql", "commands", "discord-data", "loyalty",
			"modules", "notifications", "notifications-cleanup", "projector", "transactions", "users"),
		"allow-probe-ports": sorted("commands", "discord-data", "loyalty", "modules", "notifications",
			"notifications-cleanup", "projector", "transactions", "users"),
		"allow-public-https": sorted("backup-k3s", "backup-mysql", "commands", "discord-data", "loyalty",
			"modules", "notifications", "projector", "transactions", "users"),
		"allow-heatwave": sorted("backup-mysql", "commands", "discord-data", "loyalty", "modules",
			"notifications", "transactions", "users"),
	}
	for name, apps := range want {
		assert.Equal(t, apps, requirePolicy(t, policies, name).selectedApps(t), "%s selector", name)
	}
}

func TestDBDefaultDenyGrantsTheSharedPlanes(t *testing.T) {
	base := requirePolicy(t, loadNetworkPolicies(t, dbPolicies), "default-deny-db")
	var namespaces []string
	for _, rule := range base.Spec.Egress {
		for _, to := range rule.To {
			if to.NamespaceSelector != nil {
				namespaces = append(namespaces, to.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"])
			}
		}
	}

	for _, want := range []string{"kube-system", "db", "messaging", "cache"} {
		assert.Contains(t, namespaces, want, "default-deny-db has no egress to %s", want)
	}
	assert.False(t, base.grantsEgressPort(443) || base.grantsEgressPort(3306), "default-deny-db must grant neither blanket 443 nor blanket 3306")
}

func TestDeployerPoliciesAreExact(t *testing.T) {
	policies := loadNetworkPolicies(t, deployerManifest)
	got := map[string][]string{}
	for _, name := range []string{"deployer", "allow-deployer"} {
		policy := requirePolicy(t, policies, name)
		got[name+" selects"] = []string{policy.Metadata.Namespace + " " + policy.Spec.PodSelector.render()}
		got[name+" types"] = policy.Spec.PolicyTypes
		got[name+" ingress"] = ruleSummary(policy.Spec.Ingress)
		got[name+" egress"] = ruleSummary(policy.Spec.Egress)
	}

	assert.Equal(t, map[string][]string{
		"deployer selects": {"ops app in deployer"},
		"deployer types":   {"Ingress", "Egress"},
		"deployer ingress": {"any | any | 8080/TCP"},
		"deployer egress": {"kubernetes.io/metadata.name=messaging | app in nats,nats-leaf | " +
			"4222/TCP,4223/TCP,8222/TCP,8223/TCP"},
		"allow-deployer selects": {"messaging app=nats"},
		"allow-deployer types":   {"Ingress"},
		"allow-deployer ingress": {"kubernetes.io/metadata.name=ops | app=deployer | 4222/TCP,8222/TCP"},
		"allow-deployer egress":  nil,
	}, got)
}

type ciliumNetworkPolicy struct {
	Kind     string     `yaml:"kind"`
	Metadata objectMeta `yaml:"metadata"`
	Spec     struct {
		EndpointSelector labelSelector      `yaml:"endpointSelector"`
		Ingress          []any              `yaml:"ingress"`
		Egress           []ciliumEgressRule `yaml:"egress"`
	} `yaml:"spec"`
}

type ciliumEgressRule struct {
	ToEndpoints []labelSelector `yaml:"toEndpoints"`
	ToEntities  []string        `yaml:"toEntities"`
	ToFQDNs     []struct {
		MatchName    string `yaml:"matchName"`
		MatchPattern string `yaml:"matchPattern"`
	} `yaml:"toFQDNs"`
	ToCIDR    []string `yaml:"toCIDR"`
	ToCIDRSet []struct {
		CIDR string `yaml:"cidr"`
	} `yaml:"toCIDRSet"`
	ToPorts []struct {
		Ports []struct {
			Port     string `yaml:"port"`
			Protocol string `yaml:"protocol"`
		} `yaml:"ports"`
		Rules map[string]any `yaml:"rules"`
	} `yaml:"toPorts"`
}

func (r ciliumEgressRule) peers() []string {
	var out []string
	for _, endpoints := range r.ToEndpoints {
		out = append(out, "endpoints "+endpoints.render())
	}
	for _, entity := range r.ToEntities {
		out = append(out, "entity "+entity)
	}
	for _, fqdn := range r.ToFQDNs {
		out = append(out, "fqdn "+fqdn.MatchName+fqdn.MatchPattern)
	}
	for _, cidr := range r.ToCIDR {
		out = append(out, "cidr "+cidr)
	}
	for _, set := range r.ToCIDRSet {
		out = append(out, "cidr "+set.CIDR)
	}
	if len(out) == 0 {
		out = []string{"any"}
	}
	return out
}

func (r ciliumEgressRule) ports() string {
	var out []string
	for _, toPorts := range r.ToPorts {
		for _, port := range toPorts.Ports {
			out = append(out, port.Port+"/"+port.Protocol)
		}
		if len(toPorts.Rules) > 0 {
			out = append(out, "l7")
		}
	}
	return strings.Join(out, ",")
}

func (r ciliumEgressRule) usesDNSProxy() bool {
	return len(r.ToFQDNs) > 0 || strings.Contains(r.ports(), "l7")
}

func ciliumPolicies(t *testing.T, path string) []ciliumNetworkPolicy {
	t.Helper()
	var out []ciliumNetworkPolicy
	for _, manifest := range decodeFile[ciliumNetworkPolicy](t, path) {
		if manifest.Kind == "CiliumNetworkPolicy" {
			out = append(out, manifest)
		}
	}
	return out
}

func (p ciliumNetworkPolicy) egressPeers() []string {
	var out []string
	for _, rule := range p.Spec.Egress {
		for _, peer := range rule.peers() {
			out = append(out, peer+" | "+rule.ports())
		}
	}
	slices.Sort(out)
	return out
}

func TestDeployerCiliumEgressAvoidsTheDNSProxy(t *testing.T) {
	policies := ciliumPolicies(t, deployerManifest)
	require.NotEmpty(t, policies, "deployer.yaml has no CiliumNetworkPolicy")
	policy := policies[0]

	assert.Equal(t, sorted(
		"endpoints k8s:io.kubernetes.pod.namespace=kube-system;k8s:k8s-app=kube-dns | 53/UDP,53/TCP",
		"entity kube-apiserver | 6443/TCP",
		"entity world | 443/TCP",
	), policy.egressPeers(), "any toFQDNs peer or L7 rule routes lookups through Cilium's DNS proxy, which never answers on these nodes")
	assert.Equal(t, "ops app=deployer", policy.Metadata.Namespace+" "+policy.Spec.EndpointSelector.render())
	assert.Empty(t, policy.Spec.Ingress, "deployer-egress must stay egress-only; ingress lives in the deployer NetworkPolicy")
}

func isYAMLFile(d fs.DirEntry) bool { return !d.IsDir() && strings.HasSuffix(d.Name(), ".yaml") }

func ciliumPolicyFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || !isYAMLFile(d) {
			return err
		}
		body, err := os.ReadFile(path)
		if strings.Contains(string(body), "kind: CiliumNetworkPolicy") {
			out = append(out, path)
		}
		return err
	})
	require.NoError(t, err)
	return out
}

func TestNoPolicyUsesTheCiliumDNSProxy(t *testing.T) {
	for _, path := range ciliumPolicyFiles(t) {
		for _, policy := range ciliumPolicies(t, path) {
			for _, rule := range policy.Spec.Egress {
				assert.False(t, rule.usesDNSProxy(), "%s: %s routes DNS through the Cilium proxy", path, policy.Metadata.Name)
			}
		}
	}
}
