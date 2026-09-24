// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"ItsBagelBot/internal/natsacl"
)

type Config struct {
	AccountsPath    string
	KeysPath        string
	OperatorJWTPath string
	PreloadPath     string
	DryRun          bool
	Rotate          string
}

func main() {
	cfg := parseFlags()
	if err := execute(cfg, cliDoppler{}, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "natscreds:", err)
		os.Exit(1)
	}
}

func parseFlags() Config {
	accounts := flag.String("accounts", "deploy/messaging/accounts.yaml", "path to accounts.yaml")
	keys := flag.String("keys", "deploy/messaging/accounts.keys.yaml", "path to write public key material")
	operatorJWT := flag.String("operator-jwt", "deploy/messaging/operator.jwt", "path to write the operator jwt")
	preload := flag.String("preload", "deploy/messaging/nats-accounts.conf", "path to write signed account jwts as resolver_preload")
	dryRun := flag.Bool("dry-run", false, "print planned doppler writes (names and actions only); changes nothing")
	rotate := flag.String("rotate", "", `re-mint one role's user credentials by name, or "all" for every role; never rotates keys`)
	flag.Parse()
	return Config{
		AccountsPath:    *accounts,
		KeysPath:        *keys,
		OperatorJWTPath: *operatorJWT,
		PreloadPath:     *preload,
		DryRun:          *dryRun,
		Rotate:          *rotate,
	}
}

func execute(cfg Config, store Doppler, stdout io.Writer) error {
	acl, err := natsacl.LoadACL(cfg.AccountsPath)
	if err != nil {
		return err
	}
	if cfg.DryRun {
		return runDryRun(cfg, acl, store, stdout)
	}
	return runApply(cfg, acl, store, stdout)
}

func runApply(cfg Config, acl *natsacl.ACL, store Doppler, stdout io.Writer) error {
	if err := store.EnsureProject(operatorProject); err != nil {
		return err
	}
	mat, createdKeys, err := ensureKeyMaterial(store, acl)
	if err != nil {
		return err
	}
	if err := writeOperatorArtifacts(cfg, acl, mat); err != nil {
		return err
	}
	rotate := parseRotateScope(cfg.Rotate)
	mintedCreds, err := mintAllCredentials(store, acl, mat, rotate)
	if err != nil {
		return err
	}
	printSummary(stdout, createdKeys, mintedCreds)
	return nil
}

func writeOperatorArtifacts(cfg Config, acl *natsacl.ACL, mat *material) error {
	if acl.SystemAccount == "" {
		return fmt.Errorf("natscreds: accounts.yaml has no system_account")
	}
	sysPub, err := mat.accountPub(acl.SystemAccount)
	if err != nil {
		return err
	}
	operatorToken, err := resolveOperatorJWT(cfg.OperatorJWTPath, mat, sysPub)
	if err != nil {
		return err
	}
	if err := writeOperatorJWT(cfg.OperatorJWTPath, operatorToken); err != nil {
		return err
	}
	activations, err := mintActivations(acl, mat, loadExistingKeys(cfg.KeysPath))
	if err != nil {
		return err
	}
	keys, err := buildKeys(mat, activations)
	if err != nil {
		return err
	}
	if err := writeKeysYAML(cfg.KeysPath, keys); err != nil {
		return err
	}
	return writePreload(cfg.PreloadPath, acl, keys, mat.operatorSigning)
}

// loadExistingKeys returns nil, not an error, when there is nothing to
// reuse yet: a missing or unparseable accounts.keys.yaml just means every
// activation gets minted fresh.
func loadExistingKeys(path string) *natsacl.Keys {
	keys, err := natsacl.LoadKeys(path)
	if err != nil {
		return nil
	}
	return keys
}

func mintAllCredentials(store Doppler, acl *natsacl.ACL, mat *material, rotate *rotateScope) ([]string, error) {
	targets, err := resolveRoleTargets(acl)
	if err != nil {
		return nil, err
	}
	var minted []string
	for _, target := range targets {
		created, err := mintRoleCredential(store, mat, target, rotate)
		if err != nil {
			return nil, err
		}
		if created {
			minted = append(minted, target.project+"/"+target.jwtKey)
		}
	}
	deployCreated, err := mintDeployIdentity(store, mat, rotate)
	if err != nil {
		return nil, err
	}
	if deployCreated {
		minted = append(minted, deployerProject+"/"+deploySysJWTKey)
	}
	return minted, nil
}

func printSummary(stdout io.Writer, createdKeys, mintedCreds []string) {
	for _, name := range createdKeys {
		fmt.Fprintln(stdout, "created key", name)
	}
	for _, name := range mintedCreds {
		fmt.Fprintln(stdout, "minted credential", name)
	}
	if len(createdKeys) == 0 && len(mintedCreds) == 0 {
		fmt.Fprintln(stdout, "no changes")
	}
}

func runDryRun(cfg Config, acl *natsacl.ACL, store Doppler, stdout io.Writer) error {
	rotate := parseRotateScope(cfg.Rotate)
	lines, err := planAll(acl, store, rotate)
	if err != nil {
		return err
	}
	lines = append(lines, "write "+cfg.KeysPath, "write "+cfg.OperatorJWTPath, "write "+cfg.PreloadPath)
	for _, line := range lines {
		fmt.Fprintln(stdout, line)
	}
	return nil
}

func planAll(acl *natsacl.ACL, store Doppler, rotate *rotateScope) ([]string, error) {
	keyLines, err := planKeyMaterial(store, acl)
	if err != nil {
		return nil, err
	}
	targets, err := resolveRoleTargets(acl)
	if err != nil {
		return nil, err
	}
	credLines, err := planRoleCredentials(store, targets, rotate)
	if err != nil {
		return nil, err
	}
	deployLines, err := planDeployIdentity(store, rotate)
	if err != nil {
		return nil, err
	}
	lines := append(keyLines, credLines...)
	return append(lines, deployLines...), nil
}
