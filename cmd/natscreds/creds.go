// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"errors"
	"fmt"
	"strings"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

// ErrDuplicateRoleName is returned when a role name appears in more than
// one account; --rotate and the Doppler key names both assume role names
// are globally unique, matching today's "role name == old username" rule.
var ErrDuplicateRoleName = errors.New("natscreds: duplicate role name")

// servicesProjects ports nats-secrets.py's SERVICES: a stem that owns its
// own Doppler project, named by that project's slug.
var servicesProjects = map[string]string{
	"users":          "users",
	"commands":       "commands",
	"loyalty":        "loyalty",
	"modules":        "modules",
	"projector":      "projector",
	"outgress":       "outgress",
	"worker":         "worker",
	"twitch_ingress": "twitch-ingress",
	"dashboard":      "dashboard",
	"admin":          "admin",
	"transactions":   "transactions",
	"notifications":  "notifications",
	"gossip":         "gossip",
	"discord_data":   "discord-data",
	"deployer":       "deployer",
}

// sharedProjects ports SHARED_PROJECTS: stems whose credentials share one
// Doppler project and so need a per-stem env prefix.
var sharedProjects = map[string]string{
	"discord_ingress":  "discord-svc",
	"discord_engine":   "discord-svc",
	"discord_outgress": "discord-svc",
}

// noBus ports NO_BUS: stems with no worker-bus role. NO_RPC is empty today,
// so there is no matching set.
var noBus = map[string]bool{
	"gossip":        true,
	"notifications": true,
	"transactions":  true,
	"discord_data":  true,
}

// roleRef names one role inside its account, e.g. account BUS, role
// outgress_bus.
type roleRef struct {
	account string
	role    string
}

// roleKind is the (stem, plane) pair a role name encodes, e.g. stem
// "outgress", plane "BUS".
type roleKind struct {
	stem  string
	plane string
}

// envNames is where one role's credentials live in Doppler.
type envNames struct {
	project string
	jwtKey  string
	seedKey string
}

type roleTarget struct {
	roleRef
	envNames
}

// resolveRoleTargets walks every role in acl and, for the ones that map onto
// a service (the "sys" role does not), returns where its credentials live.
func resolveRoleTargets(acl *natsacl.ACL) ([]roleTarget, error) {
	if err := validateUniqueRoleNames(acl); err != nil {
		return nil, err
	}
	var targets []roleTarget
	for _, account := range sortedAccountNames(acl) {
		for _, role := range sortedRoleNames(acl.Accounts[account].Roles) {
			target, ok, err := resolveRoleTarget(roleRef{account: account, role: role})
			if err != nil {
				return nil, err
			}
			if ok {
				targets = append(targets, target)
			}
		}
	}
	return targets, nil
}

// validateUniqueRoleNames guards the assumption --rotate and every Doppler
// key name rely on: a role name never appears under two accounts.
func validateUniqueRoleNames(acl *natsacl.ACL) error {
	seenIn := make(map[string]string)
	for _, account := range sortedAccountNames(acl) {
		for _, role := range sortedRoleNames(acl.Accounts[account].Roles) {
			if other, ok := seenIn[role]; ok {
				return fmt.Errorf("%w: %q in both %q and %q", ErrDuplicateRoleName, role, other, account)
			}
			seenIn[role] = account
		}
	}
	return nil
}

func resolveRoleTarget(ref roleRef) (roleTarget, bool, error) {
	kind, ok := parseRoleKind(ref.role)
	if !ok {
		return roleTarget{}, false, nil
	}
	if kind.plane == "BUS" && noBus[kind.stem] {
		return roleTarget{}, false, fmt.Errorf("natscreds: role %q has a bus plane but stem %q is in NO_BUS", ref.role, kind.stem)
	}
	names, ok := kind.envNames()
	if !ok {
		return roleTarget{}, false, fmt.Errorf("natscreds: role %q (stem %q) has no doppler project mapping", ref.role, kind.stem)
	}
	return roleTarget{roleRef: ref, envNames: names}, true, nil
}

func parseRoleKind(role string) (roleKind, bool) {
	switch {
	case strings.HasSuffix(role, "_bus"):
		return roleKind{stem: strings.TrimSuffix(role, "_bus"), plane: "BUS"}, true
	case strings.HasSuffix(role, "_rpc"):
		return roleKind{stem: strings.TrimSuffix(role, "_rpc"), plane: "RPC"}, true
	default:
		return roleKind{}, false
	}
}

func (k roleKind) envNames() (envNames, bool) {
	if project, ok := servicesProjects[k.stem]; ok {
		return k.plainEnvNames(project), true
	}
	if project, ok := sharedProjects[k.stem]; ok {
		return k.prefixedEnvNames(project), true
	}
	return envNames{}, false
}

func (k roleKind) plainEnvNames(project string) envNames {
	if k.plane == "BUS" {
		return envNames{project: project, jwtKey: "NATS_JWT", seedKey: "NATS_NKEY_SEED"}
	}
	return envNames{project: project, jwtKey: "NATS_RPC_JWT", seedKey: "NATS_RPC_NKEY_SEED"}
}

func (k roleKind) prefixedEnvNames(project string) envNames {
	prefix := envWord(k.stem)
	return envNames{project: project, jwtKey: prefix + "_" + k.plane + "_JWT", seedKey: prefix + "_" + k.plane + "_NKEY_SEED"}
}

type rotateScope struct {
	all  bool
	role string
}

func parseRotateScope(value string) *rotateScope {
	if value == "" {
		return nil
	}
	return &rotateScope{all: value == "all", role: value}
}

func (s *rotateScope) matches(role string) bool {
	return s != nil && (s.all || s.role == role)
}

// mintRoleCredential leaves an existing credential alone unless rotate asks
// for this role; it never touches the account or role keys.
func mintRoleCredential(store Doppler, mat *material, target roleTarget, rotate *rotateScope) (bool, error) {
	_, exists, err := store.Get(secretRef{project: target.project, key: target.jwtKey})
	if err != nil {
		return false, err
	}
	if exists && !rotate.matches(target.role) {
		return false, nil
	}
	token, seed, err := mintUserCredential(mat, target.roleRef)
	if err != nil {
		return false, err
	}
	cred := credential{jwtKey: target.jwtKey, jwtValue: token, seedKey: target.seedKey, seedValue: seed}
	if err := setCredential(store, target.project, cred); err != nil {
		return false, err
	}
	return true, nil
}

// mintUserCredential issues a user JWT signed by the role's scoped signing
// key. UserPermissionLimits must stay the zero value: nats-server's scoped
// signer check (jwt.UserScope.ValidateScopedSigner) refuses any scoped user
// whose HasEmptyPermissions() is false with "Authorization Violation", and
// jwt.NewUserClaims sets NatsLimits to NoLimit by default, which is such a
// case.
func mintUserCredential(mat *material, ref roleRef) (token, seed string, err error) {
	userKP, err := nkeys.CreateUser()
	if err != nil {
		return "", "", err
	}
	userPub, err := userKP.PublicKey()
	if err != nil {
		return "", "", err
	}
	accountPub, err := mat.accountPub(ref.account)
	if err != nil {
		return "", "", err
	}
	claims := jwt.NewUserClaims(userPub)
	claims.IssuerAccount = accountPub
	claims.UserPermissionLimits = jwt.UserPermissionLimits{}
	token, err = claims.Encode(mat.roles[ref.account][ref.role])
	if err != nil {
		return "", "", err
	}
	seedBytes, err := userKP.Seed()
	if err != nil {
		return "", "", err
	}
	return token, string(seedBytes), nil
}

func planRoleCredentials(store Doppler, targets []roleTarget, rotate *rotateScope) ([]string, error) {
	var lines []string
	for _, target := range targets {
		_, exists, err := store.Get(secretRef{project: target.project, key: target.jwtKey})
		if err != nil {
			return nil, err
		}
		lines = append(lines, roleCredentialPlanLine(target, exists, rotate))
	}
	return lines, nil
}

func roleCredentialPlanLine(target roleTarget, exists bool, rotate *rotateScope) string {
	action := "create"
	switch {
	case exists && rotate.matches(target.role):
		action = "rotate"
	case exists:
		action = "exists"
	}
	jwtRef := secretRef{project: target.project, key: target.jwtKey}
	seedRef := secretRef{project: target.project, key: target.seedKey}
	return action + " " + jwtRef.String() + " " + seedRef.String()
}
