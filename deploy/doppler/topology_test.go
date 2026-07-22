package doppler_test

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

type topology struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Parents       map[string]parent      `json:"parents"`
	Services      map[string]service     `json:"services"`
	Profiles      map[string]profile     `json:"profiles"`
	Integrations  map[string]integration `json:"integrations"`
	Rollback      []string               `json:"rollbackProjects"`
}

type parent struct {
	DirectConsumersAllowed bool                `json:"directConsumersAllowed"`
	Configs                map[string][]string `json:"configs"`
}

type service struct {
	InheritProfile string `json:"inheritProfile"`
}

type profile map[string][]string

type integration struct {
	Config                 string   `json:"config"`
	Names                  []string `json:"names"`
	KubernetesTokenSecrets []string `json:"kubernetesTokenSecrets"`
	CLIConsumers           []string `json:"cliConsumers"`
}

func loadTopology(t *testing.T) topology {
	t.Helper()
	b, err := os.ReadFile("topology.json")
	if err != nil {
		t.Fatal(err)
	}
	var got topology
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSchemaVersion(t *testing.T) {
	top := loadTopology(t)
	if top.SchemaVersion != 1 {
		t.Fatalf("unsupported schema version %d", top.SchemaVersion)
	}
}

func TestParentsAreInheritanceOnly(t *testing.T) {
	top := loadTopology(t)
	for project, parent := range top.Parents {
		if !strings.HasPrefix(project, "shared-") {
			t.Errorf("parent project %q must use the shared- prefix", project)
		}
		if parent.DirectConsumersAllowed {
			t.Errorf("parent project %q must not allow direct runtime consumers", project)
		}
	}
}

type parentConfigEntry struct {
	project string
	config  string
	names   []string
}

var rootConfigs = map[string]bool{
	"dev": true,
	"stg": true,
	"prd": true,
}

func parentConfigs(top topology) []parentConfigEntry {
	var entries []parentConfigEntry
	for project, parent := range top.Parents {
		for config, names := range parent.Configs {
			entries = append(entries, parentConfigEntry{project: project, config: config, names: names})
		}
	}
	return entries
}

func TestParentConfigsAreRootConfigsWithSortedNames(t *testing.T) {
	top := loadTopology(t)
	for _, entry := range parentConfigs(top) {
		if !rootConfigs[entry.config] {
			t.Errorf("parent %s has non-root config %q", entry.project, entry.config)
		}
		if !sort.StringsAreSorted(entry.names) {
			t.Errorf("%s/%s names must be sorted", entry.project, entry.config)
		}
	}
}

type ownedName struct {
	project string
	config  string
	name    string
}

func parentOwnedNames(top topology) []ownedName {
	var entries []ownedName
	for _, parentConfig := range parentConfigs(top) {
		for _, name := range parentConfig.names {
			entries = append(entries, ownedName{
				project: parentConfig.project,
				config:  parentConfig.config,
				name:    name,
			})
		}
	}
	return entries
}

func TestParentNameOwnershipIsDisjoint(t *testing.T) {
	top := loadTopology(t)
	owners := map[string]string{}
	for _, entry := range parentOwnedNames(top) {
		key := entry.config + "/" + entry.name
		if previous, ok := owners[key]; ok {
			t.Errorf("%s is owned by both %s and %s in %s", entry.name, previous, entry.project, entry.config)
		}
		owners[key] = entry.project
	}
}

func TestServicesUseDeclaredProfiles(t *testing.T) {
	top := loadTopology(t)
	for project, svc := range top.Services {
		if strings.HasPrefix(project, "shared") {
			t.Errorf("runtime service %q cannot be a shared parent", project)
		}
		if _, ok := top.Profiles[svc.InheritProfile]; !ok {
			t.Errorf("service %s uses missing profile %q", project, svc.InheritProfile)
		}
	}
}

type profileConfigEntry struct {
	profileName string
	childConfig string
	refs        []string
}

func profileConfigs(top topology) []profileConfigEntry {
	var entries []profileConfigEntry
	for profileName, configs := range top.Profiles {
		for childConfig, refs := range configs {
			entries = append(entries, profileConfigEntry{
				profileName: profileName,
				childConfig: childConfig,
				refs:        refs,
			})
		}
	}
	return entries
}

type profileReferenceEntry struct {
	profileName string
	childConfig string
	ref         string
}

func profileReferences(top topology) []profileReferenceEntry {
	var entries []profileReferenceEntry
	for _, config := range profileConfigs(top) {
		for _, ref := range config.refs {
			entries = append(entries, profileReferenceEntry{
				profileName: config.profileName,
				childConfig: config.childConfig,
				ref:         ref,
			})
		}
	}
	return entries
}

func parseProfileReference(raw string) (project string, config string, ok bool) {
	project, config, ok = strings.Cut(raw, ".")
	if strings.Contains(config, ".") {
		ok = false
	}
	return project, config, ok
}

func TestProfileReferencesAreWellFormed(t *testing.T) {
	for _, entry := range profileReferences(loadTopology(t)) {
		_, _, ok := parseProfileReference(entry.ref)
		if !ok {
			t.Errorf("profile %s has invalid inheritance reference %q", entry.profileName, entry.ref)
		}
	}
}

func TestProfileReferencesUseDeclaredParents(t *testing.T) {
	top := loadTopology(t)
	for _, entry := range profileReferences(top) {
		project, _, ok := parseProfileReference(entry.ref)
		if ok && top.Parents[project].Configs == nil {
			t.Errorf("profile %s references undeclared parent %q", entry.profileName, project)
		}
	}
}

func TestProfileReferencesUseDeclaredConfigs(t *testing.T) {
	top := loadTopology(t)
	for _, entry := range profileReferences(top) {
		project, config, ok := parseProfileReference(entry.ref)
		if ok && top.Parents[project].Configs[config] == nil {
			t.Errorf("profile %s references undeclared config %s", entry.profileName, entry.ref)
		}
	}
}

func expectedParentEnvironment(childConfig string) string {
	if childConfig == "dev_personal" {
		return "dev"
	}
	return childConfig
}

func TestProfileReferencesMatchChildEnvironments(t *testing.T) {
	for _, entry := range profileReferences(loadTopology(t)) {
		_, config, ok := parseProfileReference(entry.ref)
		if ok && config != expectedParentEnvironment(entry.childConfig) {
			t.Errorf("profile %s maps %s to %s", entry.profileName, entry.childConfig, entry.ref)
		}
	}
}

func TestProfileConfigsInheritEachParentOnce(t *testing.T) {
	for _, entry := range profileConfigs(loadTopology(t)) {
		assertUniqueParentReferences(t, entry)
	}
}

func assertUniqueParentReferences(t *testing.T, entry profileConfigEntry) {
	t.Helper()
	seen := map[string]bool{}
	for _, raw := range entry.refs {
		project, _, ok := parseProfileReference(raw)
		if ok && seen[project] {
			t.Errorf("profile %s/%s inherits parent %s twice", entry.profileName, entry.childConfig, project)
		}
		seen[project] = true
	}
}

func TestRollbackSourcesAreNotRuntimeServices(t *testing.T) {
	top := loadTopology(t)
	for _, project := range top.Rollback {
		if _, ok := top.Services[project]; ok {
			t.Errorf("rollback source %q is still declared as a runtime service", project)
		}
	}
}

func TestIntegrationsUseProductionRootConfigs(t *testing.T) {
	for project, integration := range loadTopology(t).Integrations {
		if integration.Config != "prd" {
			t.Errorf("integration %s must target a production root config", project)
		}
	}
}

func TestIntegrationsDeclareNames(t *testing.T) {
	for project, integration := range loadTopology(t).Integrations {
		if len(integration.Names) == 0 {
			t.Errorf("integration %s declares no names", project)
		}
	}
}

func TestIntegrationNamesAreSorted(t *testing.T) {
	for project, integration := range loadTopology(t).Integrations {
		if !sort.StringsAreSorted(integration.Names) {
			t.Errorf("integration %s names must be sorted", project)
		}
	}
}

func TestIntegrationsDeclareConsumers(t *testing.T) {
	for project, integration := range loadTopology(t).Integrations {
		if !hasIntegrationConsumer(integration) {
			t.Errorf("integration %s has no declared consumer", project)
		}
	}
}

func hasIntegrationConsumer(integration integration) bool {
	if len(integration.KubernetesTokenSecrets) != 0 {
		return true
	}
	return len(integration.CLIConsumers) != 0
}

type kubernetesBinding struct {
	project string
	value   string
}

func kubernetesBindings(top topology) []kubernetesBinding {
	var bindings []kubernetesBinding
	for project, integration := range top.Integrations {
		for _, value := range integration.KubernetesTokenSecrets {
			bindings = append(bindings, kubernetesBinding{project: project, value: value})
		}
	}
	return bindings
}

func TestIntegrationKubernetesBindingsAreNamespaced(t *testing.T) {
	for _, binding := range kubernetesBindings(loadTopology(t)) {
		if len(strings.Split(binding.value, "/")) != 2 {
			t.Errorf("integration %s has invalid Kubernetes binding %q", binding.project, binding.value)
		}
	}
}
