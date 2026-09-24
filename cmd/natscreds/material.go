// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"fmt"
	"sort"
	"strings"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/nkeys"
)

const operatorProject = "nats-operator"

// material holds every keypair the run needs, decoded from Doppler seeds.
type material struct {
	operator        nkeys.KeyPair
	operatorSigning nkeys.KeyPair
	accounts        map[string]nkeys.KeyPair
	roles           map[string]map[string]nkeys.KeyPair
}

func newMaterial(acl *natsacl.ACL) *material {
	roles := make(map[string]map[string]nkeys.KeyPair, len(acl.Accounts))
	for account, spec := range acl.Accounts {
		roles[account] = make(map[string]nkeys.KeyPair, len(spec.Roles))
	}
	return &material{
		accounts: make(map[string]nkeys.KeyPair, len(acl.Accounts)),
		roles:    roles,
	}
}

func (m *material) setRole(ref roleRef, kp nkeys.KeyPair) {
	m.roles[ref.account][ref.role] = kp
}

func (m *material) accountPub(account string) (string, error) {
	return m.accounts[account].PublicKey()
}

// keyTask is one Doppler-backed seed the run must ensure exists.
type keyTask struct {
	secretRef
	create func() (nkeys.KeyPair, error)
	assign func(nkeys.KeyPair)
}

func buildKeyTasks(acl *natsacl.ACL, mat *material) []keyTask {
	tasks := []keyTask{
		{secretRef: secretRef{project: operatorProject, key: "NATS_OPERATOR_SEED"}, create: nkeys.CreateOperator,
			assign: func(kp nkeys.KeyPair) { mat.operator = kp }},
		{secretRef: secretRef{project: operatorProject, key: "NATS_OPERATOR_SIGNING_SEED"}, create: nkeys.CreateOperator,
			assign: func(kp nkeys.KeyPair) { mat.operatorSigning = kp }},
	}
	for _, account := range sortedAccountNames(acl) {
		tasks = append(tasks, keyTask{
			secretRef: secretRef{project: operatorProject, key: accountSeedName(account)},
			create:    nkeys.CreateAccount,
			assign:    func(kp nkeys.KeyPair) { mat.accounts[account] = kp },
		})
		for _, role := range sortedRoleNames(acl.Accounts[account].Roles) {
			ref := roleRef{account: account, role: role}
			tasks = append(tasks, keyTask{
				secretRef: secretRef{project: operatorProject, key: roleSeedName(ref)},
				create:    nkeys.CreateAccount,
				assign:    func(kp nkeys.KeyPair) { mat.setRole(ref, kp) },
			})
		}
	}
	return tasks
}

func sortedAccountNames(acl *natsacl.ACL) []string {
	names := make([]string, 0, len(acl.Accounts))
	for name := range acl.Accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedRoleNames(roles map[string]natsacl.RoleSpec) []string {
	names := make([]string, 0, len(roles))
	for name := range roles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func accountSeedName(account string) string {
	return "NATS_ACCOUNT_" + envWord(account) + "_SEED"
}

func roleSeedName(ref roleRef) string {
	return "NATS_ROLE_" + envWord(ref.account) + "_" + envWord(ref.role) + "_SEED"
}

func envWord(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "-", "_"))
}

// ensureKeyMaterial fetches or mints every seed in tasks, returning the
// names it had to create so callers can report them.
func ensureKeyMaterial(store Doppler, acl *natsacl.ACL) (*material, []string, error) {
	mat := newMaterial(acl)
	var created []string
	for _, task := range buildKeyTasks(acl, mat) {
		kp, wasCreated, err := ensureSeed(store, task.secretRef, task.create)
		if err != nil {
			return nil, nil, err
		}
		task.assign(kp)
		if wasCreated {
			created = append(created, task.key)
		}
	}
	return mat, created, nil
}

func ensureSeed(store Doppler, ref secretRef, create func() (nkeys.KeyPair, error)) (nkeys.KeyPair, bool, error) {
	value, ok, err := store.Get(ref)
	if err != nil {
		return nil, false, err
	}
	if ok {
		kp, err := nkeys.FromSeed([]byte(value))
		if err != nil {
			return nil, false, fmt.Errorf("natscreds: decode seed %s: %w", ref, err)
		}
		return kp, false, nil
	}
	kp, err := create()
	if err != nil {
		return nil, false, err
	}
	seed, err := kp.Seed()
	if err != nil {
		return nil, false, err
	}
	if err := store.Set(ref, string(seed)); err != nil {
		return nil, false, err
	}
	return kp, true, nil
}

// planKeyMaterial reports, per seed, whether a run would create it, without
// minting or writing anything.
func planKeyMaterial(store Doppler, acl *natsacl.ACL) ([]string, error) {
	mat := newMaterial(acl)
	var lines []string
	for _, task := range buildKeyTasks(acl, mat) {
		_, ok, err := store.Get(task.secretRef)
		if err != nil {
			return nil, err
		}
		lines = append(lines, planLine(ok, task.secretRef))
	}
	return lines, nil
}

func planLine(exists bool, ref secretRef) string {
	action := "create"
	if exists {
		action = "exists"
	}
	return action + " " + ref.String()
}
