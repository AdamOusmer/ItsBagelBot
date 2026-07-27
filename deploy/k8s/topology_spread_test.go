package k8s

import (
	"io"
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

type workloadManifest struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Template struct {
			Spec struct {
				TopologySpreadConstraints []struct {
					MinDomains        int      `yaml:"minDomains"`
					TopologyKey       string   `yaml:"topologyKey"`
					WhenUnsatisfiable string   `yaml:"whenUnsatisfiable"`
					MatchLabelKeys    []string `yaml:"matchLabelKeys"`
				} `yaml:"topologySpreadConstraints"`
				Affinity struct {
					NodeAffinity struct {
						Required struct {
							NodeSelectorTerms []struct {
								MatchExpressions []struct {
									Key      string   `yaml:"key"`
									Operator string   `yaml:"operator"`
									Values   []string `yaml:"values"`
								} `yaml:"matchExpressions"`
							} `yaml:"nodeSelectorTerms"`
						} `yaml:"requiredDuringSchedulingIgnoredDuringExecution"`
					} `yaml:"nodeAffinity"`
				} `yaml:"affinity"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

func loadDeployment(t *testing.T, filename, name string) workloadManifest {
	t.Helper()

	f, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	for {
		var manifest workloadManifest
		if err := decoder.Decode(&manifest); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		if manifest.Kind == "Deployment" && manifest.Metadata.Name == name {
			return manifest
		}
	}

	t.Fatalf("%s Deployment is missing from %s", name, filename)
	return workloadManifest{}
}

func hostnameSpread(t *testing.T, manifest workloadManifest) struct {
	MinDomains        int      `yaml:"minDomains"`
	TopologyKey       string   `yaml:"topologyKey"`
	WhenUnsatisfiable string   `yaml:"whenUnsatisfiable"`
	MatchLabelKeys    []string `yaml:"matchLabelKeys"`
} {
	t.Helper()
	for _, constraint := range manifest.Spec.Template.Spec.TopologySpreadConstraints {
		if constraint.TopologyKey == "kubernetes.io/hostname" {
			return constraint
		}
	}
	t.Fatalf("%s has no hostname topology spread constraint", manifest.Metadata.Name)
	return struct {
		MinDomains        int      `yaml:"minDomains"`
		TopologyKey       string   `yaml:"topologyKey"`
		WhenUnsatisfiable string   `yaml:"whenUnsatisfiable"`
		MatchLabelKeys    []string `yaml:"matchLabelKeys"`
	}{}
}

func TestRolloutSensitiveWorkloadsHardSpreadEachReplicaSet(t *testing.T) {
	tests := []struct {
		file       string
		name       string
		minDomains int
	}{
		{"console-admin.yaml", "console-admin", 0},
		// minDomains tracks the number of schedulable app-tier nodes, which is 3.
		// It must NEVER exceed that count: when it does, skew is measured against
		// a global minimum of zero and every replica becomes unschedulable, so an
		// over-large value is a total outage rather than a loose constraint. This
		// was 4 while the fleet had four app nodes, and only kept working after
		// the move because the cordoned old nodes still counted as domains
		// (nodeTaintsPolicy defaults to Ignore) — deleting them would have taken
		// both of these services to zero replicas.
		{"notifications.yaml", "notifications", 3},
		{"transactions.yaml", "transactions", 3},
		{"twitch-ingress.yaml", "twitch-ingress", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constraint := hostnameSpread(t, loadDeployment(t, tt.file, tt.name))
			if constraint.WhenUnsatisfiable != "DoNotSchedule" {
				t.Fatalf("hostname spread is %q, want DoNotSchedule", constraint.WhenUnsatisfiable)
			}
			if !slices.Contains(constraint.MatchLabelKeys, "pod-template-hash") {
				t.Fatal("hostname spread must be scoped to the incoming ReplicaSet")
			}
			if constraint.MinDomains != tt.minDomains {
				t.Fatalf("minDomains = %d, want %d", constraint.MinDomains, tt.minDomains)
			}
		})
	}
}

// excludesWorkerPool captures the placement rule under test: a nodeAffinity
// match expression that keeps the workload off worker-role nodes.
//
// The key moved from itsbagelbot.dev/pool=worker-pool to
// itsbagelbot.dev/role=worker when node roles were unified onto one key
// (cp / node / worker). No worker-role node exists yet, so this guard is
// currently inert — it is kept expressed on the live key so that it starts
// holding the moment one is added, rather than silently pointing at a retired
// label that nothing would ever match.
func excludesWorkerPool(key, operator string, values []string) bool {
	return key == "itsbagelbot.dev/role" &&
		operator == "NotIn" &&
		slices.Contains(values, "worker")
}

func TestConsoleAdminExplicitlyExcludesWorkerPool(t *testing.T) {
	admin := loadDeployment(t, "console-admin.yaml", "console-admin")
	terms := admin.Spec.Template.Spec.Affinity.NodeAffinity.Required.NodeSelectorTerms

	for _, term := range terms {
		for _, expression := range term.MatchExpressions {
			if excludesWorkerPool(expression.Key, expression.Operator, expression.Values) {
				return
			}
		}
	}

	t.Fatal("console-admin must explicitly exclude the worker pool")
}
