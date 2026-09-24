// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

const (
	deployerProject      = "deployer"
	deploySysAccount     = "SYS"
	deploySysRoleName    = "deploy_sys"
	deploySysJWTKey      = "DEPLOY_NATS_SYS_JWT"
	deploySysSeedKey     = "DEPLOY_NATS_SYS_NKEY_SEED"
	deploySigningSeedKey = "DEPLOY_NATS_SIGNING_SEED"
)

// mintDeployIdentity gives the deployer a SYS user that can push account
// claims updates and look up their propagation, nothing else, plus a copy of
// the operator signing seed it needs to sign those claims before pushing.
func mintDeployIdentity(store Doppler, mat *material, rotate *rotateScope) (bool, error) {
	if err := ensureSigningSeedCopy(store, mat); err != nil {
		return false, err
	}
	_, exists, err := store.Get(secretRef{project: deployerProject, key: deploySysJWTKey})
	if err != nil {
		return false, err
	}
	if exists && !rotate.matches(deploySysRoleName) {
		return false, nil
	}
	token, seed, err := mintDeploySysUser(mat)
	if err != nil {
		return false, err
	}
	cred := credential{jwtKey: deploySysJWTKey, jwtValue: token, seedKey: deploySysSeedKey, seedValue: seed}
	if err := setCredential(store, deployerProject, cred); err != nil {
		return false, err
	}
	return true, nil
}

func ensureSigningSeedCopy(store Doppler, mat *material) error {
	signingSeed, err := mat.operatorSigning.Seed()
	if err != nil {
		return err
	}
	return ensureCopiedSecret(store, secretRef{project: deployerProject, key: deploySigningSeedKey}, string(signingSeed))
}

func mintDeploySysUser(mat *material) (token, seed string, err error) {
	userKP, err := nkeys.CreateUser()
	if err != nil {
		return "", "", err
	}
	userPub, err := userKP.PublicKey()
	if err != nil {
		return "", "", err
	}
	claims := jwt.NewUserClaims(userPub)
	claims.Pub = jwt.Permission{Allow: []string{
		"$SYS.REQ.CLAIMS.UPDATE",
		"$SYS.REQ.ACCOUNT.*.CLAIMS.LOOKUP",
		"$SYS.REQ.SERVER.PING",
	}}
	claims.Sub = jwt.Permission{Allow: []string{"_INBOX.>"}}
	token, err = claims.Encode(mat.accounts[deploySysAccount])
	if err != nil {
		return "", "", err
	}
	seedBytes, err := userKP.Seed()
	if err != nil {
		return "", "", err
	}
	return token, string(seedBytes), nil
}

func planDeployIdentity(store Doppler, rotate *rotateScope) ([]string, error) {
	jwtRef := secretRef{project: deployerProject, key: deploySysJWTKey}
	_, exists, err := store.Get(jwtRef)
	if err != nil {
		return nil, err
	}
	action := "create"
	switch {
	case exists && rotate.matches(deploySysRoleName):
		action = "rotate"
	case exists:
		action = "exists"
	}
	seedRef := secretRef{project: deployerProject, key: deploySysSeedKey}
	return []string{action + " " + jwtRef.String() + " " + seedRef.String()}, nil
}
