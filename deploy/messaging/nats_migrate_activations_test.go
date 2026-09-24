// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

// populateActivations mints a real activation JWT, signed by the exporter's
// generated key, for every restricted export's importer and records it in
// ring.keys.Activations so natsacl.Compile can attach it to the matching
// import; Compile fails with ErrMissingActivation without this.
func populateActivations(t *testing.T, acl *natsacl.ACL, ring *aclKeyring) {
	t.Helper()
	for exporter, spec := range acl.Accounts {
		for _, exp := range spec.Exports {
			if len(exp.Accounts) == 0 {
				continue
			}
			grantActivations(t, ring, exporter, exp)
		}
	}
}

func grantActivations(t *testing.T, ring *aclKeyring, exporter string, exp natsacl.ExportSpec) {
	t.Helper()
	subject := exportSubject(exp)
	for _, importer := range exp.Accounts {
		token := mintActivation(t, ring.accounts[exporter], ring.keys.Accounts[importer], exp)
		ring.keys.Activations[importer] = append(ring.keys.Activations[importer],
			natsacl.Activation{From: exporter, Subject: subject, Token: token})
	}
}

func exportKind(exp natsacl.ExportSpec) jwt.ExportType {
	if exp.Stream != "" {
		return jwt.Stream
	}
	return jwt.Service
}

func mintActivation(t *testing.T, exporter nkeys.KeyPair, importerPub string, exp natsacl.ExportSpec) string {
	t.Helper()
	ac := jwt.NewActivationClaims(importerPub)
	ac.ImportSubject = jwt.Subject(exportSubject(exp))
	ac.ImportType = exportKind(exp)
	token, err := ac.Encode(exporter)
	if err != nil {
		t.Fatalf("mint activation for %s: %v", exportSubject(exp), err)
	}
	return token
}
