// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mountPath string

type hardening struct {
	RunAsNonRoot             *bool `yaml:"runAsNonRoot"`
	AllowPrivilegeEscalation *bool `yaml:"allowPrivilegeEscalation"`
	ReadOnlyRootFilesystem   *bool `yaml:"readOnlyRootFilesystem"`
	SeccompProfile           struct {
		Type string `yaml:"type"`
	} `yaml:"seccompProfile"`
	Capabilities struct {
		Drop []string `yaml:"drop"`
	} `yaml:"capabilities"`
}

type hardenedContainer struct {
	Name            string    `yaml:"name"`
	Image           string    `yaml:"image"`
	SecurityContext hardening `yaml:"securityContext"`
	VolumeMounts    []struct {
		Name      string    `yaml:"name"`
		MountPath mountPath `yaml:"mountPath"`
	} `yaml:"volumeMounts"`
}

type hardenedPod struct {
	SecurityContext hardening           `yaml:"securityContext"`
	InitContainers  []hardenedContainer `yaml:"initContainers"`
	Containers      []hardenedContainer `yaml:"containers"`
	Volumes         []podVolume         `yaml:"volumes"`
}

type podVolume struct {
	Name     string `yaml:"name"`
	EmptyDir *struct {
		SizeLimit string `yaml:"sizeLimit"`
	} `yaml:"emptyDir"`
}

type hardenedWorkload struct {
	Kind     string     `yaml:"kind"`
	Metadata objectMeta `yaml:"metadata"`
	Spec     struct {
		Template struct {
			Spec hardenedPod `yaml:"spec"`
		} `yaml:"template"`
		JobTemplate struct {
			Spec struct {
				Template struct {
					Spec hardenedPod `yaml:"spec"`
				} `yaml:"template"`
			} `yaml:"spec"`
		} `yaml:"jobTemplate"`
	} `yaml:"spec"`
}

func (w hardenedWorkload) pod() hardenedPod {
	if w.Kind == "CronJob" {
		return w.Spec.JobTemplate.Spec.Template.Spec
	}
	return w.Spec.Template.Spec
}

type containerPosture struct {
	ReadOnlyRoot   bool
	NoEscalation   bool
	DropsAll       bool
	RuntimeDefault bool
	NonRoot        bool
	Scratch        []mountPath
}

func (p hardenedPod) posture(c hardenedContainer) containerPosture {
	return containerPosture{
		ReadOnlyRoot:   isTrue(c.SecurityContext.ReadOnlyRootFilesystem),
		NoEscalation:   c.SecurityContext.AllowPrivilegeEscalation != nil && !*c.SecurityContext.AllowPrivilegeEscalation,
		DropsAll:       slices.Contains(c.SecurityContext.Capabilities.Drop, "ALL"),
		RuntimeDefault: firstNonEmpty(c.SecurityContext.SeccompProfile.Type, p.SecurityContext.SeccompProfile.Type) == "RuntimeDefault",
		NonRoot:        isTrue(firstSet(c.SecurityContext.RunAsNonRoot, p.SecurityContext.RunAsNonRoot)),
		Scratch:        p.scratchMounts(c),
	}
}

func (p hardenedPod) scratchMounts(c hardenedContainer) []mountPath {
	var out []mountPath
	for _, m := range c.VolumeMounts {
		if p.isEmptyDir(m.Name) {
			out = append(out, m.MountPath)
		}
	}
	return out
}

func (p hardenedPod) isEmptyDir(volume string) bool {
	i := slices.IndexFunc(p.Volumes, func(v podVolume) bool { return v.Name == volume })
	return i >= 0 && p.Volumes[i].EmptyDir != nil
}

func (p hardenedPod) unboundedScratch() []string {
	var out []string
	for _, v := range p.Volumes {
		if v.EmptyDir != nil && v.EmptyDir.SizeLimit == "" {
			out = append(out, v.Name)
		}
	}
	return out
}

func isTrue(b *bool) bool { return b != nil && *b }

func firstSet(values ...*bool) *bool {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func hardened(scratch ...mountPath) containerPosture {
	return containerPosture{ReadOnlyRoot: true, NoEscalation: true, DropsAll: true, RuntimeDefault: true, NonRoot: true, Scratch: scratch}
}

// warp-state predates the size limits and holds the vendor daemon's diagnostic files; a limit would evict gossip.
var unboundedScratchAllowed = map[string][]string{"app/gossip": {"warp-state"}}

func TestFirstPartyContainersAreHardened(t *testing.T) {
	warp := hardened("/var/lib/cloudflare-warp", "/run/cloudflare-warp")
	warp.NonRoot = false
	want := map[string]containerPosture{
		"db/admin/admin":                         hardened(),
		"app/automod/automod":                    hardened(),
		"ops/clusterops/clusterops":              hardened(),
		"db/commands/commands":                   hardened(),
		"app/console-admin/console-admin":        hardened(),
		"app/console-dashboard/console-dashboard": hardened(),
		"app/gossip/gossip":                      hardened(),
		"app/gossip/warp":                        warp,
		"db/loyalty/loyalty":                     hardened(),
		"db/modules/modules":                     hardened(),
		"db/notifications/notifications":         hardened(),
		"db/notifications-cleanup/cleanup":       hardened(),
		"app/outgress/outgress":                  hardened(),
		"db/projector/projector":                 hardened(),
		"app/sesame/sesame":                      hardened(),
		"db/transactions/transactions":           hardened(),
		"app/twitch-ingress/ingress":             hardened(),
		"db/users/users":                         hardened(),
	}

	got := map[string]containerPosture{}
	unbounded := map[string][]string{}
	for _, filename := range manifestFilenames(t) {
		for _, w := range decodeFile[hardenedWorkload](t, filename) {
			collectFirstParty(w, got, unbounded)
		}
	}

	assert.Equal(t, want, got)
	assert.Equal(t, unboundedScratchAllowed, unbounded, "give every new emptyDir a sizeLimit")
}

func collectFirstParty(w hardenedWorkload, got map[string]containerPosture, unbounded map[string][]string) {
	pod := w.pod()
	key := w.Metadata.Namespace + "/" + w.Metadata.Name
	firstParty := false
	for _, c := range append(slices.Clone(pod.InitContainers), pod.Containers...) {
		if strings.HasPrefix(c.Image, "ghcr.io/adamousmer/") {
			got[key+"/"+c.Name] = pod.posture(c)
			firstParty = true
		}
	}
	if names := pod.unboundedScratch(); firstParty && len(names) > 0 {
		unbounded[key] = names
	}
}
