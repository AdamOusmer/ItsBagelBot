// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var bothDirections = sorted("Egress", "Ingress")

func TestLockedNamespacesDenyByDefault(t *testing.T) {
	for _, namespace := range []string{"cache", "cert-manager", "keda", "networking", "observability", "tailscale"} {
		policies := loadNetworkPolicies(t, "../"+namespace+"/network-policies.yaml")
		policy := requirePolicy(t, policies, "default-deny-"+namespace)
		if selector := policy.Spec.PodSelector.render(); selector != "" {
			t.Errorf("default-deny-%s selects %q, want every pod", namespace, selector)
		}
		requireBothDirections(t, policy)
	}
}

func TestOperatorsPoliciesCoverBothDirections(t *testing.T) {
	policies := loadNetworkPolicies(t, "../operators/network-policies.yaml")
	for _, name := range []string{"doppler-operator", "cloudflared"} {
		requireBothDirections(t, requirePolicy(t, policies, name))
	}
}

func requireBothDirections(t *testing.T, policy networkPolicyManifest) {
	t.Helper()
	if got := sorted(policy.Spec.PolicyTypes...); !slices.Equal(got, bothDirections) {
		t.Errorf("%s/%s policyTypes = %v, want %v", policy.Metadata.Namespace, policy.Metadata.Name, got, bothDirections)
	}
}

// Cilium's DNS proxy never answers on these nodes: a toFQDNs peer or an L7
// rule in any policy cuts that workload's DNS entirely.
func TestNoPolicyUsesTheCiliumDNSProxy(t *testing.T) {
	for _, path := range ciliumPolicyFiles(t) {
		for _, policy := range decodeFile[ciliumNetworkPolicy](t, path) {
			if policy.Kind == "CiliumNetworkPolicy" {
				requireNoDNSProxy(t, path, policy)
			}
		}
	}
}

func requireNoDNSProxy(t *testing.T, path string, policy ciliumNetworkPolicy) {
	t.Helper()
	for _, rule := range policy.Spec.Egress {
		if len(rule.ToFQDNs) > 0 || strings.Contains(rule.ports(), "l7") {
			t.Errorf("%s: %s routes DNS through the Cilium proxy", path, policy.Metadata.Name)
		}
	}
}

func ciliumPolicyFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !isYAMLFile(d) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), "kind: CiliumNetworkPolicy") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func isYAMLFile(d fs.DirEntry) bool {
	return !d.IsDir() && strings.HasSuffix(d.Name(), ".yaml")
}
