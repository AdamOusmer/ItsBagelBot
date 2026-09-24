// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"time"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
)

// exportGrantInfo is the (subject, kind) an export or import resolves to.
type exportGrantInfo struct {
	subject string
	kind    jwt.ExportType
}

// activationKey names one activation: exporter, importer, and the grant it
// covers.
type activationKey struct {
	exporter string
	importer string
	grant    exportGrantInfo
}

// activationCtx bundles the two things resolving an activation needs: the
// key material to mint a fresh one, and the previous file's activations to
// reuse from.
type activationCtx struct {
	mat      *material
	existing *natsacl.Keys
}

// mintActivations issues one activation token per (importer, exporter,
// subject) that an accounts-gated export lists, reusing a token from
// existing when it still verifies so an unchanged export keeps its exact
// bytes; existing may be nil on a first run. It touches no Doppler secret:
// the tokens are public and are recorded straight into accounts.keys.yaml.
func mintActivations(acl *natsacl.ACL, mat *material, existing *natsacl.Keys) (map[string][]natsacl.Activation, error) {
	ctx := activationCtx{mat: mat, existing: existing}
	activations := make(map[string][]natsacl.Activation)
	for _, exporter := range sortedAccountNames(acl) {
		for _, exp := range acl.Accounts[exporter].Exports {
			if len(exp.Accounts) == 0 {
				continue
			}
			if err := mintExportActivations(activations, ctx, exporter, exp); err != nil {
				return nil, err
			}
		}
	}
	return activations, nil
}

func mintExportActivations(activations map[string][]natsacl.Activation, ctx activationCtx, exporter string, exp natsacl.ExportSpec) error {
	grant := exportGrant(exp)
	for _, importer := range exp.Accounts {
		key := activationKey{exporter: exporter, importer: importer, grant: grant}
		token, err := resolveActivationToken(ctx, key)
		if err != nil {
			return err
		}
		activations[importer] = append(activations[importer], natsacl.Activation{From: exporter, Subject: grant.subject, Token: token})
	}
	return nil
}

func resolveActivationToken(ctx activationCtx, key activationKey) (string, error) {
	if token, ok := reusableActivationToken(ctx, key); ok {
		return token, nil
	}
	return mintActivationToken(ctx.mat, key)
}

func reusableActivationToken(ctx activationCtx, key activationKey) (string, bool) {
	if ctx.existing == nil {
		return "", false
	}
	for _, a := range ctx.existing.Activations[key.importer] {
		if a.From == key.exporter && a.Subject == key.grant.subject {
			return validActivationToken(ctx.mat, key, a.Token)
		}
	}
	return "", false
}

func validActivationToken(mat *material, key activationKey, token string) (string, bool) {
	claims, err := jwt.DecodeActivationClaims(token)
	if err != nil {
		return "", false
	}
	if !activationClaimsMatch(mat, key, claims) {
		return "", false
	}
	if claims.Expires > 0 && time.Now().Unix() > claims.Expires {
		return "", false
	}
	return token, true
}

func activationClaimsMatch(mat *material, key activationKey, claims *jwt.ActivationClaims) bool {
	exporterPub, err := mat.accountPub(key.exporter)
	if err != nil {
		return false
	}
	importerPub, err := mat.accountPub(key.importer)
	if err != nil {
		return false
	}
	if claims.Issuer != exporterPub || claims.Subject != importerPub {
		return false
	}
	return string(claims.ImportSubject) == key.grant.subject && claims.ImportType == key.grant.kind
}

func mintActivationToken(mat *material, key activationKey) (string, error) {
	importerPub, err := mat.accountPub(key.importer)
	if err != nil {
		return "", err
	}
	claims := jwt.NewActivationClaims(importerPub)
	claims.ImportSubject = jwt.Subject(key.grant.subject)
	claims.ImportType = key.grant.kind
	return claims.Encode(mat.accounts[key.exporter])
}

func exportGrant(exp natsacl.ExportSpec) exportGrantInfo {
	if exp.Service != "" {
		return exportGrantInfo{subject: exp.Service, kind: jwt.Service}
	}
	return exportGrantInfo{subject: exp.Stream, kind: jwt.Stream}
}
