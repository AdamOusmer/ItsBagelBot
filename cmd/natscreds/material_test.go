// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/nkeys"
)

func smallTestACL() *natsacl.ACL {
	return &natsacl.ACL{Accounts: map[string]natsacl.AccountSpec{
		"BUS": {Roles: map[string]natsacl.RoleSpec{"outgress_bus": {}, "commands_bus": {}}},
		"SYS": {Roles: map[string]natsacl.RoleSpec{"sys": {}}},
	}}
}

func TestBuildKeyTasksCoversEverySeed(t *testing.T) {
	acl := smallTestACL()
	mat := newMaterial(acl)
	tasks := buildKeyTasks(acl, mat)
	want := 2 + 2 + 3 // operator + operator signing, 2 accounts, 3 roles
	if len(tasks) != want {
		t.Fatalf("got %d tasks, want %d", len(tasks), want)
	}
	names := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if names[task.key] {
			t.Fatalf("duplicate task name %s", task.key)
		}
		names[task.key] = true
	}
	for _, want := range []string{
		"NATS_OPERATOR_SEED", "NATS_OPERATOR_SIGNING_SEED",
		"NATS_ACCOUNT_BUS_SEED", "NATS_ACCOUNT_SYS_SEED",
		"NATS_ROLE_BUS_OUTGRESS_BUS_SEED", "NATS_ROLE_BUS_COMMANDS_BUS_SEED", "NATS_ROLE_SYS_SYS_SEED",
	} {
		if !names[want] {
			t.Errorf("missing task %s", want)
		}
	}
}

func TestEnsureSeedIsIdempotentAndReusesTheSamePublicKey(t *testing.T) {
	store := newFakeDoppler()
	ref := secretRef{project: "nats-operator", key: "NATS_ACCOUNT_BUS_SEED"}
	kp1, created, err := ensureSeed(store, ref, nkeys.CreateAccount)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	pub1, _ := kp1.PublicKey()

	kp2, created, err := ensureSeed(store, ref, nkeys.CreateAccount)
	if err != nil || created {
		t.Fatalf("second call: created=%v err=%v, want reuse", created, err)
	}
	pub2, _ := kp2.PublicKey()
	if pub1 != pub2 {
		t.Fatalf("public key changed across ensureSeed calls: %s != %s", pub1, pub2)
	}
	if store.setCalls != 1 {
		t.Fatalf("setCalls = %d, want 1", store.setCalls)
	}
}

func TestPlanKeyMaterialMarksExistingVsMissing(t *testing.T) {
	acl := smallTestACL()
	store := newFakeDoppler()
	seedExistingSecret(t, store, operatorProject, "NATS_ACCOUNT_BUS_SEED")

	lines, err := planKeyMaterial(store, acl)
	if err != nil {
		t.Fatal(err)
	}
	if store.setCalls != 1 {
		t.Fatalf("plan made %d Set calls beyond the seeded fixture, want 0", store.setCalls-1)
	}
	assertContains(t, lines, "exists nats-operator/NATS_ACCOUNT_BUS_SEED")
	assertContains(t, lines, "create nats-operator/NATS_ACCOUNT_SYS_SEED")
}

func seedExistingSecret(t *testing.T, store *fakeDoppler, project, key string) {
	t.Helper()
	kp, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	seed, err := kp.Seed()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(secretRef{project: project, key: key}, string(seed)); err != nil {
		t.Fatal(err)
	}
}
