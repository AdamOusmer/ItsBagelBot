package k8s

import (
	"io"
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

type networkPolicyManifest struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		PodSelector struct {
			MatchExpressions []struct {
				Key    string   `yaml:"key"`
				Values []string `yaml:"values"`
			} `yaml:"matchExpressions"`
		} `yaml:"podSelector"`
		Egress []struct {
			To []struct {
				NamespaceSelector *struct {
					MatchLabels map[string]string `yaml:"matchLabels"`
				} `yaml:"namespaceSelector"`
				IPBlock *struct {
					CIDR string `yaml:"cidr"`
				} `yaml:"ipBlock"`
			} `yaml:"to"`
			Ports []struct {
				Port     int    `yaml:"port"`
				Protocol string `yaml:"protocol"`
			} `yaml:"ports"`
		} `yaml:"egress"`
	} `yaml:"spec"`
}

func loadNetworkPolicies(t *testing.T) map[string]networkPolicyManifest {
	t.Helper()
	f, err := os.Open("network-policies.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	policies := make(map[string]networkPolicyManifest)
	decoder := yaml.NewDecoder(f)
	for {
		var manifest networkPolicyManifest
		if err := decoder.Decode(&manifest); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		if manifest.Kind == "NetworkPolicy" {
			policies[manifest.Metadata.Name] = manifest
		}
	}
	return policies
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
	base := requirePolicy(t, loadNetworkPolicies(t), "default-deny-apps")
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
	publicHTTPS := requirePolicy(t, loadNetworkPolicies(t), "allow-public-https")
	wantHTTPS := sorted("commands", "console-admin", "console-dashboard", "gateway", "loyalty", "modules", "notifications", "outgress", "projector", "sesame", "transactions", "twitch-ingress", "users")
	if got := selectedApps(t, publicHTTPS); !slices.Equal(got, wantHTTPS) {
		t.Fatalf("public HTTPS allowlist = %v, want %v", got, wantHTTPS)
	}
}

func TestHeatWaveEgressAllowlist(t *testing.T) {
	heatwave := requirePolicy(t, loadNetworkPolicies(t), "allow-heatwave")
	wantHeatWave := sorted("commands", "console-admin", "loyalty", "modules", "notifications", "transactions", "users")
	if got := selectedApps(t, heatwave); !slices.Equal(got, wantHeatWave) {
		t.Fatalf("HeatWave allowlist = %v, want %v", got, wantHeatWave)
	}
	if len(heatwave.Spec.Egress) != 1 {
		t.Fatal("HeatWave policy must have exactly one egress rule")
	}
	if len(heatwave.Spec.Egress[0].To) != 1 {
		t.Fatal("HeatWave rule must have exactly one destination")
	}
	ipBlock := heatwave.Spec.Egress[0].To[0].IPBlock
	if ipBlock == nil || ipBlock.CIDR != "10.0.0.0/16" {
		t.Fatal("HeatWave egress must stay confined to the routed OCI private subnet")
	}
}
