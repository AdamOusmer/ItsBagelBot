// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
)

// mintActivations issues one activation token per (importer, exporter,
// subject) that an accounts-gated export lists, signed by the exporter's
// account identity key. It touches no Doppler secret: the tokens are public
// and are recorded straight into accounts.keys.yaml.
func mintActivations(acl *natsacl.ACL, mat *material) (map[string][]natsacl.Activation, error) {
	activations := make(map[string][]natsacl.Activation)
	for _, exporter := range sortedAccountNames(acl) {
		for _, exp := range acl.Accounts[exporter].Exports {
			if len(exp.Accounts) == 0 {
				continue
			}
			if err := mintExportActivations(activations, mat, exporter, exp); err != nil {
				return nil, err
			}
		}
	}
	return activations, nil
}

func mintExportActivations(activations map[string][]natsacl.Activation, mat *material, exporter string, exp natsacl.ExportSpec) error {
	grant := exportGrant(exp)
	for _, importer := range exp.Accounts {
		token, err := mintActivationToken(mat, exporter, importer, grant)
		if err != nil {
			return err
		}
		activations[importer] = append(activations[importer], natsacl.Activation{From: exporter, Subject: grant.subject, Token: token})
	}
	return nil
}

// exportGrantInfo is the (subject, kind) an export or import resolves to.
type exportGrantInfo struct {
	subject string
	kind    jwt.ExportType
}

func mintActivationToken(mat *material, exporter, importer string, grant exportGrantInfo) (string, error) {
	importerPub, err := mat.accountPub(importer)
	if err != nil {
		return "", err
	}
	claims := jwt.NewActivationClaims(importerPub)
	claims.ImportSubject = jwt.Subject(grant.subject)
	claims.ImportType = grant.kind
	return claims.Encode(mat.accounts[exporter])
}

func exportGrant(exp natsacl.ExportSpec) exportGrantInfo {
	if exp.Service != "" {
		return exportGrantInfo{subject: exp.Service, kind: jwt.Service}
	}
	return exportGrantInfo{subject: exp.Stream, kind: jwt.Stream}
}
