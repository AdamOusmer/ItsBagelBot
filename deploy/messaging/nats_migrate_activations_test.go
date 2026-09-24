// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

// activationGrant mints and installs the activation tokens a restricted
// export (one naming specific importer accounts) requires before the
// importer's compiled Import will actually establish.
type activationGrant struct {
	ring   *aclKeyring
	byName map[string]*jwt.AccountClaims
}

// mintActivations finds every export restricted to specific accounts and,
// for each importer, signs an activation with the exporter's own key and
// installs it on the importer's compiled Import so the account resolves.
func mintActivations(t *testing.T, acl *natsacl.ACL, ring *aclKeyring, claims []*jwt.AccountClaims) {
	t.Helper()
	grant := activationGrant{ring: ring, byName: claimsByName(claims)}
	for exporter, spec := range acl.Accounts {
		for _, exp := range spec.Exports {
			if len(exp.Accounts) == 0 {
				continue
			}
			grant.apply(t, exporter, exp)
		}
	}
}

// exportGrant is what an activation certifies: a subject and kind exported
// by one account for a specific importer to use.
type exportGrant struct {
	subject string
	kind    jwt.ExportType
}

func (g activationGrant) apply(t *testing.T, exporter string, exp natsacl.ExportSpec) {
	t.Helper()
	grant := exportSubject(exp)
	exporterPub := g.ring.keys.Accounts[exporter]
	for _, importer := range exp.Accounts {
		token := mintActivation(t, g.ring.accounts[exporter], g.ring.keys.Accounts[importer], grant)
		installImportToken(g.byName[importer], exporterPub, grant.subject, token)
	}
}

func exportSubject(exp natsacl.ExportSpec) exportGrant {
	if exp.Stream != "" {
		return exportGrant{subject: exp.Stream, kind: jwt.Stream}
	}
	return exportGrant{subject: exp.Service, kind: jwt.Service}
}

func mintActivation(t *testing.T, exporter nkeys.KeyPair, importerPub string, grant exportGrant) string {
	t.Helper()
	ac := jwt.NewActivationClaims(importerPub)
	ac.ImportSubject = jwt.Subject(grant.subject)
	ac.ImportType = grant.kind
	token, err := ac.Encode(exporter)
	if err != nil {
		t.Fatalf("mint activation for %s: %v", grant.subject, err)
	}
	return token
}

func installImportToken(claims *jwt.AccountClaims, exporterPub, subject, token string) {
	for _, imp := range claims.Imports {
		if imp.Account == exporterPub && string(imp.Subject) == subject {
			imp.Token = token
		}
	}
}
