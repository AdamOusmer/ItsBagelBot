// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	appPolicies      = "network-policies.yaml"
	dbPolicies       = "../db/network-policies.yaml"
	deployerManifest = "deployer.yaml"
)

type networkPolicyManifest struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Spec struct {
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

func decodeFile[T any](t *testing.T, path string) []T {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var out []T
	decoder := yaml.NewDecoder(f)
	for {
		var manifest T
		err := decoder.Decode(&manifest)
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, manifest)
	}
}

func selectedApps(t *testing.T, policy networkPolicyManifest) []string {
	t.Helper()
	for _, expression := range policy.Spec.PodSelector.MatchExpressions {
		if expression.Key == "app" {
			apps := slices.Clone(expression.Values)
			slices.Sort(apps)
			return apps
		}
	}
	t.Fatal("policy has no app selector")
	return nil
}

func sorted(values ...string) []string {
	slices.Sort(values)
	return values
}

func requirePolicy(t *testing.T, policies map[string]networkPolicyManifest, name string) networkPolicyManifest {
	t.Helper()
	policy, ok := policies[name]
	if !ok {
		t.Fatalf("%s policy is missing", name)
	}
	return policy
}

func policyHasPort(policy networkPolicyManifest, target int) bool {
	for _, rule := range policy.Spec.Egress {
		for _, port := range rule.Ports {
			if port.Port == target {
				return true
			}
		}
	}
	return false
}

func TestDefaultPolicyHasNoBlanketExternalEgress(t *testing.T) {
	base := requirePolicy(t, loadNetworkPolicies(t, appPolicies), "default-deny-apps")
	if !slices.Contains(selectedApps(t, base), "notifications-cleanup") {
		t.Fatal("notifications cleanup job escaped the default-deny policy")
	}
	if policyHasPort(base, 443) {
		t.Fatal("default policy grants blanket external port 443")
	}
	if policyHasPort(base, 3306) {
		t.Fatal("default policy grants blanket external port 3306")
	}
}

func TestPublicHTTPSEgressAllowlist(t *testing.T) {
	publicHTTPS := requirePolicy(t, loadNetworkPolicies(t, appPolicies), "allow-public-https")
	wantHTTPS := sorted("commands", "console-admin", "console-dashboard", "discord-ingress", "discord-outgress", "gossip", "loyalty", "modules", "notifications", "outgress", "projector", "sesame", "transactions", "twitch-ingress", "users")
	if got := selectedApps(t, publicHTTPS); !slices.Equal(got, wantHTTPS) {
		t.Fatalf("public HTTPS allowlist = %v, want %v", got, wantHTTPS)
	}
}

func TestHeatWaveEgressAllowlist(t *testing.T) {
	heatwave := requirePolicy(t, loadNetworkPolicies(t, appPolicies), "allow-heatwave")
	wantHeatWave := sorted("commands", "console-admin", "loyalty", "modules", "notifications", "transactions", "users")
	if got := selectedApps(t, heatwave); !slices.Equal(got, wantHeatWave) {
		t.Fatalf("HeatWave allowlist = %v, want %v", got, wantHeatWave)
	}
	if len(heatwave.Spec.Egress) != 1 {
		t.Fatal("HeatWave policy must have exactly one egress rule")
	}
	var got []string
	for _, to := range heatwave.Spec.Egress[0].To {
		if to.IPBlock == nil {
			t.Fatal("HeatWave egress destinations must all be ipBlocks")
		}
		got = append(got, to.IPBlock.CIDR)
	}
	want := sorted("10.0.0.0/16", "204.216.107.73/32")
	if !slices.Equal(sorted(got...), want) {
		t.Fatalf("HeatWave egress CIDRs = %v, want %v", got, want)
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
		if got := selectedApps(t, requirePolicy(t, policies, name)); !slices.Equal(got, apps) {
			t.Fatalf("%s selector = %v, want %v", name, got, apps)
		}
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
		if !slices.Contains(namespaces, want) {
			t.Fatalf("default-deny-db has no egress to %s, got %v", want, namespaces)
		}
	}
	if policyHasPort(base, 443) || policyHasPort(base, 3306) {
		t.Fatal("default-deny-db must grant neither blanket 443 nor blanket 3306")
	}
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
		ports := renderPorts(rule.Ports)
		for _, peer := range peers {
			out = append(out, peer.NamespaceSelector.render()+" | "+peer.PodSelector.render()+" | "+ports)
		}
	}
	return out
}

func renderPorts(ports []policyPort) string {
	out := make([]string, len(ports))
	for i, port := range ports {
		out[i] = fmt.Sprintf("%d/%s", port.Port, cmp.Or(port.Protocol, "TCP"))
	}
	return strings.Join(out, ",")
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
	want := map[string][]string{
		"deployer selects": {"ops app in deployer"},
		"deployer types":   {"Ingress", "Egress"},
		"deployer ingress": {"any | any | 8080/TCP"},
		"deployer egress": {"kubernetes.io/metadata.name=messaging | app in nats,nats-leaf | " +
			"4222/TCP,4223/TCP,8222/TCP,8223/TCP"},
		"allow-deployer selects": {"messaging app=nats"},
		"allow-deployer types":   {"Ingress"},
		"allow-deployer ingress": {"kubernetes.io/metadata.name=ops | app=deployer | 4222/TCP,8222/TCP"},
		"allow-deployer egress":  nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deployer network policies:\n got %v\nwant %v", got, want)
	}
}

type ciliumNetworkPolicy struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Spec struct {
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
	}
	return strings.Join(out, ",")
}

func deployerCiliumPolicy(t *testing.T) ciliumNetworkPolicy {
	t.Helper()
	for _, manifest := range decodeFile[ciliumNetworkPolicy](t, deployerManifest) {
		if manifest.Kind == "CiliumNetworkPolicy" {
			return manifest
		}
	}
	t.Fatal("deployer.yaml has no CiliumNetworkPolicy")
	return ciliumNetworkPolicy{}
}

func TestDeployerInternetEgressIsHostScoped(t *testing.T) {
	policy := deployerCiliumPolicy(t)
	var got []string
	for _, rule := range policy.Spec.Egress {
		ports := rule.ports()
		for _, peer := range rule.peers() {
			got = append(got, peer+" | "+ports)
		}
	}
	slices.Sort(got)
	hosts := []string{
		"endpoints k8s:io.kubernetes.pod.namespace=kube-system;k8s:k8s-app=kube-dns | 53/UDP,53/TCP",
		"entity kube-apiserver | 6443/TCP",
		"fqdn *.actions.githubusercontent.com | 443/TCP",
		"fqdn *.itsbagelbot.com | 443/TCP",
		"fqdn api.github.com | 443/TCP",
		"fqdn ghcr.io | 443/TCP",
		"fqdn pkg-containers.githubusercontent.com | 443/TCP",
	}
	for i := range 20 {
		hosts = append(hosts, fmt.Sprintf("fqdn productionresultssa%d.blob.core.windows.net | 443/TCP", i))
	}
	want := sorted(hosts...)
	scope := policy.Metadata.Namespace + " " + policy.Spec.EndpointSelector.render()
	if !slices.Equal(got, want) || scope != "ops app=deployer" {
		t.Fatalf("deployer Cilium egress (%s):\n got %v\nwant %v", scope, got, want)
	}
	if len(policy.Spec.Ingress) != 0 {
		t.Fatal("deployer-egress must stay egress-only; ingress lives in the deployer NetworkPolicy")
	}
}
