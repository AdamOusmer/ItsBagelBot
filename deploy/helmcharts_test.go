// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package deploy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type helmChart struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Spec struct {
		Chart         string `yaml:"chart"`
		Repo          string `yaml:"repo"`
		Version       string `yaml:"version"`
		ValuesContent string `yaml:"valuesContent"`
	} `yaml:"spec"`
}

var exactVersion = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func loadHelmCharts(t *testing.T) map[string]helmChart {
	t.Helper()
	charts := map[string]helmChart{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		for {
			var doc helmChart
			if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
				return nil
			} else if err != nil {
				return nil
			}
			if doc.Kind == "HelmChart" {
				require.NotContains(t, charts, doc.Metadata.Name, "duplicate HelmChart %s (%s)", doc.Metadata.Name, path)
				charts[doc.Metadata.Name] = doc
			}
		}
	})
	require.NoError(t, err)
	return charts
}

// k3s's helm-controller re-reconciles every HelmChart object whenever the
// server restarts. Any chart in kube-system that git does not hold gets
// reverted to whatever stale spec the object carries.
func TestEveryHelmChartInUseIsTrackedInGit(t *testing.T) {
	charts := loadHelmCharts(t)

	names := make([]string, 0, len(charts))
	for name := range charts {
		names = append(names, name)
	}
	slices.Sort(names)
	require.Equal(t, []string{"falco", "newrelic-bundle", "tailscale-operator", "traefik"}, names)

	for name, c := range charts {
		require.Equal(t, "kube-system", c.Metadata.Namespace, name)
		require.NotEmpty(t, c.Spec.Chart, name)
		require.NotEmpty(t, c.Spec.Repo, name)
		require.Regexp(t, exactVersion, c.Spec.Version, "%s must pin an exact chart version", name)

		var values map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(c.Spec.ValuesContent), &values), name)
		require.NotEmpty(t, values, name)
	}
}

func TestTailscaleOperatorIsPatchedRelease(t *testing.T) {
	c := loadHelmCharts(t)["tailscale-operator"]
	var major, minor, patch int
	_, err := fmt.Sscanf(c.Spec.Version, "%d.%d.%d", &major, &minor, &patch)
	require.NoError(t, err)
	require.Equal(t, 1, major)
	require.Equal(t, 104, minor)
	require.GreaterOrEqual(t, patch, 2, "1.104.2 carries the TS-2026-013 fix")
}

func TestNewRelicBundleKeepsControlPlaneToleration(t *testing.T) {
	c := loadHelmCharts(t)["newrelic-bundle"]
	require.Equal(t, 5, strings.Count(c.Spec.ValuesContent, "value: cp"),
		"controlplane, ksm, kubelet, kube-state-metrics and prometheus-agent must tolerate the cp1 taint")
}

func TestFalcoUIStaysOff(t *testing.T) {
	c := loadHelmCharts(t)["falco"]
	var values struct {
		Sidekick struct {
			WebUI struct {
				Enabled bool `yaml:"enabled"`
			} `yaml:"webui"`
		} `yaml:"falcosidekick"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(c.Spec.ValuesContent), &values))
	require.False(t, values.Sidekick.WebUI.Enabled)
}
