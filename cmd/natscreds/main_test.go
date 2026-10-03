// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const realAccountsYAML = "../../deploy/messaging/accounts.yaml"

const (
	wantAccounts  = 20
	wantRoles     = 33
	wantKeySeeds  = 2 + wantAccounts + wantRoles
	wantRoleCreds = 32 // 33 roles minus "sys", which owns no service credential
)

type harness struct {
	cfg   Config
	store *fakeDoppler
	out   bytes.Buffer
}

func newHarness(t *testing.T, accountsYAML string) *harness {
	t.Helper()
	dir := t.TempDir()
	h := &harness{
		cfg: Config{
			AccountsPath:    realAccountsYAML,
			KeysPath:        filepath.Join(dir, "accounts.keys.yaml"),
			OperatorJWTPath: filepath.Join(dir, "operator.jwt"),
			PreloadPath:     filepath.Join(dir, "nats-accounts.conf"),
		},
		store: newFakeDoppler(),
	}
	if accountsYAML != "" {
		h.cfg.AccountsPath = filepath.Join(dir, "accounts.yaml")
		h.setACL(t, accountsYAML)
	}
	return h
}

func (h *harness) setACL(t *testing.T, accountsYAML string) {
	t.Helper()
	require.NoError(t, os.WriteFile(h.cfg.AccountsPath, []byte(accountsYAML), 0o600))
}

func (h *harness) apply(t *testing.T) {
	t.Helper()
	h.out.Reset()
	require.NoError(t, execute(h.cfg, h.store, &h.out))
}

func (h *harness) applyErr() error {
	h.out.Reset()
	return execute(h.cfg, h.store, &h.out)
}

func (h *harness) keys(t *testing.T) *natsacl.Keys {
	t.Helper()
	keys, err := natsacl.LoadKeys(h.cfg.KeysPath)
	require.NoError(t, err)
	return keys
}

func (h *harness) secret(project, key string) string { return h.store.secrets[project][key] }

func (h *harness) operatorJWT(t *testing.T) string {
	t.Helper()
	token, err := os.ReadFile(h.cfg.OperatorJWTPath)
	require.NoError(t, err)
	return strings.TrimSpace(string(token))
}

func (h *harness) publicFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, path := range []string{h.cfg.KeysPath, h.cfg.OperatorJWTPath, h.cfg.PreloadPath} {
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		info, err := os.Stat(path)
		require.NoError(t, err)
		files[filepath.Base(path)] = string(content) + "@" + info.ModTime().String()
	}
	return files
}

func (h *harness) backdatePublicFiles(t *testing.T) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	for _, path := range []string{h.cfg.KeysPath, h.cfg.OperatorJWTPath, h.cfg.PreloadPath} {
		require.NoError(t, os.Chtimes(path, old, old))
	}
}

func snapshotSecrets(store *fakeDoppler) map[string]string {
	out := make(map[string]string)
	for project, kv := range store.secrets {
		for key, value := range kv {
			out[project+"/"+key] = value
		}
	}
	return out
}

func changedSecrets(before, after map[string]string) []string {
	var changed []string
	for key, value := range after {
		if before[key] != value {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	return changed
}

func TestFirstRunCreatesEveryKeyAndCredential(t *testing.T) {
	h := newHarness(t, "")

	h.apply(t)

	assert.Equal(t, wantKeySeeds+wantRoleCreds*2+3, h.store.setCalls)
	keys := h.keys(t)
	assert.Len(t, keys.Accounts, wantAccounts)
	assert.Len(t, keys.Activations["ADMIN_RPC"], 2)
}

func TestSecondRunChangesOnlyWhatWasRotated(t *testing.T) {
	tests := []struct {
		name        string
		rotate      string
		wantChanged []string
	}{
		{name: "re-running changes no secret", wantChanged: nil},
		{name: "rotating one role changes only that role's credential", rotate: "outgress_bus",
			wantChanged: []string{"outgress/NATS_JWT", "outgress/NATS_NKEY_SEED"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, "")
			h.apply(t)
			h.backdatePublicFiles(t)
			secretsBefore, filesBefore := snapshotSecrets(h.store), h.publicFiles(t)

			h.cfg.Rotate = tc.rotate
			h.apply(t)

			assert.Equal(t, tc.wantChanged, changedSecrets(secretsBefore, snapshotSecrets(h.store)))
			assert.Equal(t, filesBefore, h.publicFiles(t), "public files must stay byte-identical and untouched")
		})
	}
}

func TestRotatingAllReissuesEveryCredentialButNoKeySeed(t *testing.T) {
	h := newHarness(t, "")
	h.apply(t)
	before := snapshotSecrets(h.store)

	h.cfg.Rotate = "all"
	h.apply(t)

	changed := changedSecrets(before, snapshotSecrets(h.store))
	assert.Len(t, changed, wantRoleCreds*2+2)
	for _, key := range changed {
		assert.False(t, strings.HasPrefix(key, operatorProject+"/"), "rotation must never rotate keys: %s", key)
	}
}

func TestRunsNeverPrintASecret(t *testing.T) {
	h := newHarness(t, "")

	h.apply(t)
	applyOutput := h.out.String()
	h.cfg.DryRun = true
	h.apply(t)

	for _, printed := range []string{applyOutput, h.out.String()} {
		for key, value := range snapshotSecrets(h.store) {
			assert.NotContains(t, printed, value, "printed output leaked the value of %s", key)
		}
	}
}

func TestDryRunPlansEveryWriteWithoutMutatingTheStore(t *testing.T) {
	h := newHarness(t, "")
	h.cfg.DryRun = true

	h.apply(t)

	assert.Zero(t, h.store.setCalls)
	assert.Empty(t, h.store.projects)
	assert.Contains(t, h.out.String(), "create nats-operator/NATS_OPERATOR_SEED")
	assert.Contains(t, h.out.String(), "write "+h.cfg.KeysPath)

	h.cfg.DryRun, h.cfg.Rotate = false, ""
	h.apply(t)
	h.cfg.DryRun, h.cfg.Rotate = true, "outgress_bus"
	h.apply(t)

	assert.Contains(t, h.out.String(), "exists nats-operator/NATS_ACCOUNT_BUS_SEED")
	assert.Contains(t, h.out.String(), "exists users/NATS_JWT users/NATS_NKEY_SEED")
	assert.Contains(t, h.out.String(), "rotate outgress/NATS_JWT outgress/NATS_NKEY_SEED")
}

func TestGeneratedArtifactsAreConsistent(t *testing.T) {
	h := newHarness(t, "")
	h.apply(t)
	keys := h.keys(t)

	t.Run("keys compile against the accounts", func(t *testing.T) {
		acl, err := natsacl.LoadACL(h.cfg.AccountsPath)
		require.NoError(t, err)

		claims, err := natsacl.Compile(acl, keys)

		require.NoError(t, err)
		assert.Len(t, claims, wantAccounts)
	})
	t.Run("operator jwt lists the signing key and system account", func(t *testing.T) {
		claims, err := jwt.DecodeOperatorClaims(h.operatorJWT(t))
		require.NoError(t, err)

		assert.Equal(t, keys.Operator, claims.Subject)
		assert.Len(t, claims.SigningKeys, 1)
		assert.Equal(t, keys.Accounts["SYS"], claims.SystemAccount)
	})
}

func TestOperatorJWTIsReusedUntilTheSystemAccountChanges(t *testing.T) {
	withSystemAccount := func(name string) string {
		return fmt.Sprintf("system_account: %s\naccounts:\n  SYS: {}\n  OTHER: {}\n", name)
	}
	h := newHarness(t, withSystemAccount("SYS"))
	h.apply(t)
	first := h.operatorJWT(t)

	h.apply(t)
	assert.Equal(t, first, h.operatorJWT(t), "a matching operator jwt must be reused verbatim")

	h.setACL(t, withSystemAccount("OTHER"))
	h.apply(t)

	reissued := h.operatorJWT(t)
	assert.NotEqual(t, first, reissued)
	claims, err := jwt.DecodeOperatorClaims(reissued)
	require.NoError(t, err)
	assert.Equal(t, h.keys(t).Accounts["OTHER"], claims.SystemAccount)
}
