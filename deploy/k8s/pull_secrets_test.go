// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type pullContainer struct {
	Image string `yaml:"image"`
}

type pullPodSpec struct {
	ServiceAccountName string          `yaml:"serviceAccountName"`
	Containers         []pullContainer `yaml:"containers"`
}

type pullManifest struct {
	Kind     string     `yaml:"kind"`
	Metadata objectMeta `yaml:"metadata"`
	Secrets  []struct {
		Name string `yaml:"name"`
	} `yaml:"imagePullSecrets"`
	Spec struct {
		Template struct {
			Spec pullPodSpec `yaml:"spec"`
		} `yaml:"template"`
		JobTemplate struct {
			Spec struct {
				Template struct {
					Spec pullPodSpec `yaml:"spec"`
				} `yaml:"template"`
			} `yaml:"spec"`
		} `yaml:"jobTemplate"`
	} `yaml:"spec"`
}

func (m pullManifest) pod() pullPodSpec {
	if m.Kind == "CronJob" {
		return m.Spec.JobTemplate.Spec.Template.Spec
	}
	return m.Spec.Template.Spec
}

func (m pullManifest) carriesPullSecret() bool {
	for _, s := range m.Secrets {
		if s.Name == "ghcr-pull" {
			return true
		}
	}
	return false
}

func (p pullPodSpec) firstParty() bool {
	for _, c := range p.Containers {
		if strings.HasPrefix(c.Image, "ghcr.io/adamousmer/itsbagelbot/") {
			return true
		}
	}
	return false
}

// The packages are private: a pod on its own service account pulls nothing unless that account carries ghcr-pull.
func TestOwnServiceAccountsCarryThePullSecret(t *testing.T) {
	var manifests []pullManifest
	for _, filename := range manifestFilenames(t) {
		manifests = append(manifests, decodeFile[pullManifest](t, filename)...)
	}
	pulls := pullingAccounts(manifests)

	var missing []string
	for _, m := range manifests {
		if account, needs := m.ownAccount(); needs && !pulls[account] {
			missing = append(missing, m.Kind+" "+m.Metadata.Namespace+"/"+m.Metadata.Name+" runs as "+account)
		}
	}
	assert.Empty(t, missing, "first-party images need ghcr-pull on the service account they run as")
}

func pullingAccounts(manifests []pullManifest) map[string]bool {
	pulls := map[string]bool{}
	for _, m := range manifests {
		if m.Kind == "ServiceAccount" {
			pulls[m.Metadata.Namespace+"/"+m.Metadata.Name] = m.carriesPullSecret()
		}
	}
	return pulls
}

func (m pullManifest) ownAccount() (string, bool) {
	pod := m.pod()
	if pod.ServiceAccountName == "" {
		return "", false
	}
	return m.Metadata.Namespace + "/" + pod.ServiceAccountName, pod.firstParty()
}
