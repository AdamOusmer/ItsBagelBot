// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

var operatorSigningRef = secretRef{project: operatorProject, key: "NATS_OPERATOR_SIGNING_SEED"}

func runPreloadOnly(cfg Config, acl *natsacl.ACL, store Doppler, stdout io.Writer) error {
	if cfg.Rotate != "" {
		return fmt.Errorf("natscreds: --preload-only cannot rotate credentials")
	}
	keys, err := natsacl.LoadKeys(cfg.KeysPath)
	if err != nil {
		return err
	}
	if _, err := natsacl.Compile(acl, keys); err != nil {
		return err
	}
	signer, err := existingPreloadSigner(store)
	if err != nil {
		return err
	}
	defer signer.Wipe()
	if err := authorizePreloadSigner(cfg, acl, keys, signer); err != nil {
		return err
	}
	if cfg.DryRun {
		fmt.Fprintln(stdout, "write", cfg.PreloadPath)
		return nil
	}
	return writePreload(cfg.PreloadPath, acl, keys, signer)
}

func existingPreloadSigner(store Doppler) (nkeys.KeyPair, error) {
	seed, exists, err := store.Get(operatorSigningRef)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("natscreds: existing operator signing key is required")
	}
	key, err := nkeys.FromSeed([]byte(seed))
	if err != nil {
		return nil, fmt.Errorf("natscreds: invalid operator signing key: %w", err)
	}
	return key, nil
}

func authorizePreloadSigner(cfg Config, acl *natsacl.ACL, keys *natsacl.Keys, signer nkeys.KeyPair) error {
	body, err := os.ReadFile(cfg.OperatorJWTPath)
	if err != nil {
		return err
	}
	claims, err := jwt.DecodeOperatorClaims(strings.TrimSpace(string(body)))
	if err != nil {
		return err
	}
	public, err := signer.PublicKey()
	if err != nil {
		return err
	}
	if !preloadOperatorMatches(claims, keys) {
		return fmt.Errorf("natscreds: operator JWT does not match existing operator key")
	}
	if claims.SystemAccount != keys.Accounts[acl.SystemAccount] {
		return fmt.Errorf("natscreds: operator JWT does not match existing system account")
	}
	if !slices.Contains([]string(claims.SigningKeys), public) {
		return fmt.Errorf("natscreds: signing key is not authorized by operator JWT")
	}
	return nil
}

func preloadOperatorMatches(claims *jwt.OperatorClaims, keys *natsacl.Keys) bool {
	return claims.Issuer == keys.Operator && claims.Subject == keys.Operator
}
