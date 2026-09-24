// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
)

const realAccountsYAML = "../../deploy/messaging/accounts.yaml"

const (
	wantAccounts  = 20
	wantRoles     = 33
	wantKeySeeds  = 2 + wantAccounts + wantRoles
	wantRoleCreds = 32 // 33 roles minus "sys", which owns no service credential
)

func testConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	return Config{
		AccountsPath:    realAccountsYAML,
		KeysPath:        filepath.Join(dir, "accounts.keys.yaml"),
		OperatorJWTPath: filepath.Join(dir, "operator.jwt"),
	}
}

func TestFirstRunCreatesEveryKeyAndCredential(t *testing.T) {
	cfg := testConfig(t)
	store := newFakeDoppler()
	var out bytes.Buffer

	if err := execute(cfg, store, &out); err != nil {
		t.Fatal(err)
	}

	wantSets := wantKeySeeds + wantRoleCreds*2 + 3
	if store.setCalls != wantSets {
		t.Fatalf("setCalls = %d, want %d", store.setCalls, wantSets)
	}

	keys, err := natsacl.LoadKeys(cfg.KeysPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys.Accounts) != wantAccounts {
		t.Fatalf("accounts.keys.yaml has %d accounts, want %d", len(keys.Accounts), wantAccounts)
	}
	if got := len(keys.Activations["ADMIN_RPC"]); got != 2 {
		t.Fatalf("ADMIN_RPC activations = %d, want 2", got)
	}
}

func TestSecondRunIsNoOp(t *testing.T) {
	cfg := testConfig(t)
	store := newFakeDoppler()
	var out bytes.Buffer

	if err := execute(cfg, store, &out); err != nil {
		t.Fatal(err)
	}
	afterFirst := store.setCalls

	if err := execute(cfg, store, &out); err != nil {
		t.Fatal(err)
	}
	if store.setCalls != afterFirst {
		t.Fatalf("second run made %d more Set calls, want 0", store.setCalls-afterFirst)
	}
}

func TestRotateScopeChangesOnlyThatRole(t *testing.T) {
	cfg := testConfig(t)
	store := newFakeDoppler()
	var out bytes.Buffer
	if err := execute(cfg, store, &out); err != nil {
		t.Fatal(err)
	}
	before := snapshotSecrets(store)

	cfg.Rotate = "outgress_bus"
	if err := execute(cfg, store, &out); err != nil {
		t.Fatal(err)
	}
	after := snapshotSecrets(store)

	changed := diffKeys(before, after)
	want := []string{"outgress/NATS_JWT", "outgress/NATS_NKEY_SEED"}
	if !sameSet(changed, want) {
		t.Fatalf("changed secrets = %v, want exactly %v", changed, want)
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

func diffKeys(before, after map[string]string) []string {
	var changed []string
	for key, value := range after {
		if before[key] != value {
			changed = append(changed, key)
		}
	}
	return changed
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, v := range a {
		seen[v] = true
	}
	for _, v := range b {
		if !seen[v] {
			return false
		}
	}
	return true
}

func TestRunsNeverPrintASecret(t *testing.T) {
	cfg := testConfig(t)
	store := newFakeDoppler()
	var out bytes.Buffer

	if err := execute(cfg, store, &out); err != nil {
		t.Fatal(err)
	}
	assertNoSecretLeaked(t, store, out.String())

	out.Reset()
	dryCfg := cfg
	dryCfg.DryRun = true
	if err := execute(dryCfg, store, &out); err != nil {
		t.Fatal(err)
	}
	assertNoSecretLeaked(t, store, out.String())
}

func assertNoSecretLeaked(t *testing.T, store *fakeDoppler, printed string) {
	t.Helper()
	for project, kv := range store.secrets {
		for key, value := range kv {
			if value == "" {
				continue
			}
			if strings.Contains(printed, value) {
				t.Fatalf("printed output leaked the value of %s/%s", project, key)
			}
		}
	}
}

func TestDryRunNeverMutatesStore(t *testing.T) {
	cfg := testConfig(t)
	cfg.DryRun = true
	store := newFakeDoppler()
	var out bytes.Buffer

	if err := execute(cfg, store, &out); err != nil {
		t.Fatal(err)
	}
	if store.setCalls != 0 {
		t.Fatalf("dry-run made %d Set calls, want 0", store.setCalls)
	}
	if len(store.projects) != 0 {
		t.Fatalf("dry-run created projects %v, want none", store.projects)
	}
	if out.Len() == 0 {
		t.Fatal("dry-run printed nothing")
	}
}

func TestGeneratedKeysCompile(t *testing.T) {
	cfg := testConfig(t)
	store := newFakeDoppler()
	var out bytes.Buffer
	if err := execute(cfg, store, &out); err != nil {
		t.Fatal(err)
	}

	acl, err := natsacl.LoadACL(cfg.AccountsPath)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := natsacl.LoadKeys(cfg.KeysPath)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := natsacl.Compile(acl, keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != wantAccounts {
		t.Fatalf("compiled %d accounts, want %d", len(claims), wantAccounts)
	}
}

func TestOperatorJWTListsSigningKeyAndSystemAccount(t *testing.T) {
	cfg := testConfig(t)
	store := newFakeDoppler()
	var out bytes.Buffer
	if err := execute(cfg, store, &out); err != nil {
		t.Fatal(err)
	}

	keys, err := natsacl.LoadKeys(cfg.KeysPath)
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(cfg.OperatorJWTPath)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.DecodeOperatorClaims(strings.TrimSpace(string(token)))
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != keys.Operator {
		t.Fatalf("Subject = %s, want operator key %s", claims.Subject, keys.Operator)
	}
	if len(claims.SigningKeys) != 1 {
		t.Fatalf("SigningKeys = %v, want exactly one", claims.SigningKeys)
	}
	if claims.SystemAccount != keys.Accounts["SYS"] {
		t.Fatalf("SystemAccount = %s, want %s", claims.SystemAccount, keys.Accounts["SYS"])
	}
}
