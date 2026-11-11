// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type signingKeyReader struct {
	seed   string
	reads  []secretRef
	writes int
}

func (s *signingKeyReader) EnsureProject(dopplerProject) error {
	s.writes++
	return fmt.Errorf("project writes forbidden")
}

func (s *signingKeyReader) Get(ref secretRef) (string, bool, error) {
	s.reads = append(s.reads, ref)
	if ref != operatorSigningRef {
		return "", false, fmt.Errorf("credential reads forbidden")
	}
	return s.seed, s.seed != "", nil
}

func (s *signingKeyReader) Set(secretRef, string) error {
	s.writes++
	return fmt.Errorf("secret writes forbidden")
}

const preloadInitialACL = "system_account: SYS\naccounts:\n  SYS: {}\n  BUS: {}\n"
const preloadUpdatedACL = "system_account: SYS\naccounts:\n  SYS: {}\n  BUS:\n    exports:\n      - service: rpc.probe\n"

func TestPreloadOnlyUsesExistingIdentityWithoutCredentialWrites(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		dryRun  bool
		exports int
	}{
		{"write public preload", false, 1},
		{"dry run", true, 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			h := newHarness(t, preloadInitialACL)
			h.apply(t)
			h.setACL(t, preloadUpdatedACL)
			h.cfg.PreloadOnly, h.cfg.DryRun = true, scenario.dryRun
			store := &signingKeyReader{seed: h.secret(operatorProject, operatorSigningRef.key)}
			before := h.publicFiles(t)

			require.NoError(t, execute(h.cfg, store, &h.out))
			after := h.publicFiles(t)
			assert.Equal(t, []string{before["accounts.keys.yaml"], before["operator.jwt"]}, []string{after["accounts.keys.yaml"], after["operator.jwt"]})
			assert.Equal(t, scenario.exports, preloadedExports(t, h.cfg.PreloadPath, h.keys(t).Accounts["BUS"]))
			assert.Equal(t, struct {
				Reads  []secretRef
				Writes int
			}{[]secretRef{operatorSigningRef}, 0}, struct {
				Reads  []secretRef
				Writes int
			}{store.reads, store.writes})

			require.NoError(t, execute(h.cfg, store, &h.out))
			assert.Equal(t, after, h.publicFiles(t))
		})
	}
}

func TestPreloadOnlyRejectsMissingOrUnauthorizedSigningKeys(t *testing.T) {
	for _, scenario := range []struct {
		name string
		seed string
		want string
	}{
		{"missing key", "", "existing operator signing key is required"},
		{"invalid key", "invalid", "invalid operator signing key"},
		{"unauthorized key", newOperatorSeed(t), "not authorized by operator JWT"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			h := newHarness(t, preloadInitialACL)
			h.apply(t)
			h.cfg.PreloadOnly = true
			before := h.publicFiles(t)
			store := &signingKeyReader{seed: scenario.seed}

			assert.ErrorContains(t, execute(h.cfg, store, &h.out), scenario.want)
			assert.Equal(t, before, h.publicFiles(t))
			assert.Zero(t, store.writes)
		})
	}
}

func newOperatorSeed(t *testing.T) string {
	t.Helper()
	key, err := nkeys.CreateOperator()
	require.NoError(t, err)
	defer key.Wipe()
	seed, err := key.Seed()
	require.NoError(t, err)
	return string(seed)
}

func TestPreloadOnlyRefusesRotationAndForeignOperatorIdentity(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		configure func(*harness)
		want      string
	}{
		{"rotation", func(h *harness) { h.cfg.Rotate = "all" }, "cannot rotate credentials"},
		{"foreign operator", func(h *harness) {
			token := newHarness(t, preloadInitialACL)
			token.apply(t)
			body, err := os.ReadFile(token.cfg.OperatorJWTPath)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(h.cfg.OperatorJWTPath, body, 0o600))
		}, "operator JWT does not match existing operator key"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			h := newHarness(t, preloadInitialACL)
			h.apply(t)
			h.cfg.PreloadOnly = true
			scenario.configure(h)
			store := &signingKeyReader{seed: h.secret(operatorProject, operatorSigningRef.key)}

			assert.ErrorContains(t, execute(h.cfg, store, &h.out), scenario.want)
			assert.Zero(t, store.writes)
		})
	}
}
