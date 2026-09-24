// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

func findActivationToken(activations map[string][]natsacl.Activation, importer, exporter string) string {
	for _, a := range activations[importer] {
		if a.From == exporter {
			return a.Token
		}
	}
	return ""
}

func gatedTestACL() *natsacl.ACL {
	return &natsacl.ACL{Accounts: map[string]natsacl.AccountSpec{
		"DEPLOYER_RPC": {Exports: []natsacl.ExportSpec{
			{Service: "bagel.rpc.admin.deploy.>", Accounts: []string{"ADMIN_RPC"}},
			{Stream: "bagel.deploy.events.>", Accounts: []string{"ADMIN_RPC"}},
		}},
		"ADMIN_RPC": {Imports: []natsacl.ImportSpec{
			{Service: "bagel.rpc.admin.deploy.>", From: "DEPLOYER_RPC"},
			{Stream: "bagel.deploy.events.>", From: "DEPLOYER_RPC"},
		}},
	}}
}

func TestMintActivationsVerifyAgainstExporter(t *testing.T) {
	acl := gatedTestACL()
	mat := testMaterialFor(t, acl)

	activations, err := mintActivations(acl, mat, nil)
	if err != nil {
		t.Fatal(err)
	}
	entries := activations["ADMIN_RPC"]
	if len(entries) != 2 {
		t.Fatalf("ADMIN_RPC activations = %d, want 2", len(entries))
	}
	for _, entry := range entries {
		assertActivation(t, mat, "ADMIN_RPC", entry)
	}
}

func assertActivation(t *testing.T, mat *material, importer string, entry natsacl.Activation) {
	t.Helper()
	exporterPub, err := mat.accountPub(entry.From)
	if err != nil {
		t.Fatal(err)
	}
	importerPub, err := mat.accountPub(importer)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwt.DecodeActivationClaims(entry.Token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != exporterPub {
		t.Errorf("Issuer = %s, want exporter %s", claims.Issuer, exporterPub)
	}
	if claims.Subject != importerPub {
		t.Errorf("Subject = %s, want importer %s", claims.Subject, importerPub)
	}
	if string(claims.ImportSubject) != entry.Subject {
		t.Errorf("ImportSubject = %s, want %s", claims.ImportSubject, entry.Subject)
	}
	if claims.ImportType != wantActivationType(entry.Subject) {
		t.Errorf("subject %s: ImportType = %s, want %s", entry.Subject, claims.ImportType, wantActivationType(entry.Subject))
	}
}

func wantActivationType(subject string) jwt.ExportType {
	if subject == "bagel.deploy.events.>" {
		return jwt.Stream
	}
	return jwt.Service
}

func twoExporterGatedACL(exporterASubject string) *natsacl.ACL {
	return &natsacl.ACL{Accounts: map[string]natsacl.AccountSpec{
		"EXPORTER_A": {Exports: []natsacl.ExportSpec{{Service: exporterASubject, Accounts: []string{"IMPORTER"}}}},
		"EXPORTER_B": {Exports: []natsacl.ExportSpec{{Service: "svc.b.>", Accounts: []string{"IMPORTER"}}}},
		"IMPORTER":   {},
	}}
}

// TestMintActivationsReusesUnchangedAndReissuesChangedExport is the second-
// run contract for accounts.keys.yaml: only the activation whose export
// restriction actually changed gets a new token, byte for byte.
func TestMintActivationsReusesUnchangedAndReissuesChangedExport(t *testing.T) {
	before := twoExporterGatedACL("svc.a.>")
	mat := testMaterialFor(t, before)

	firstRun, err := mintActivations(before, mat, nil)
	if err != nil {
		t.Fatal(err)
	}
	tokenABefore := findActivationToken(firstRun, "IMPORTER", "EXPORTER_A")
	tokenBBefore := findActivationToken(firstRun, "IMPORTER", "EXPORTER_B")
	if tokenABefore == "" || tokenBBefore == "" {
		t.Fatal("expected activations for both exporters on the first run")
	}

	after := twoExporterGatedACL("svc.a2.>")
	existing := &natsacl.Keys{Activations: firstRun}
	secondRun, err := mintActivations(after, mat, existing)
	if err != nil {
		t.Fatal(err)
	}
	tokenAAfter := findActivationToken(secondRun, "IMPORTER", "EXPORTER_A")
	tokenBAfter := findActivationToken(secondRun, "IMPORTER", "EXPORTER_B")

	if tokenAAfter == tokenABefore {
		t.Fatal("EXPORTER_A's activation must be reissued once its export subject changes")
	}
	if tokenBAfter != tokenBBefore {
		t.Fatal("EXPORTER_B's activation must be reused byte-for-byte since nothing about it changed")
	}
}

// TestMintActivationsRejectsReuseOfAWronglySignedToken guards
// activationClaimsMatch specifically: reusableActivationToken's own (from,
// subject) lookup would happily hand this token to it, so only the claims
// check inside catches that it was never actually signed by the exporter's
// current key.
func TestMintActivationsRejectsReuseOfAWronglySignedToken(t *testing.T) {
	acl := gatedTestACL()
	mat := testMaterialFor(t, acl)
	importerPub, err := mat.accountPub("ADMIN_RPC")
	if err != nil {
		t.Fatal(err)
	}
	wrongKP, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.NewActivationClaims(importerPub)
	claims.ImportSubject = jwt.Subject("bagel.rpc.admin.deploy.>")
	claims.ImportType = jwt.Service
	badToken, err := claims.Encode(wrongKP)
	if err != nil {
		t.Fatal(err)
	}
	existing := &natsacl.Keys{Activations: map[string][]natsacl.Activation{
		"ADMIN_RPC": {{From: "DEPLOYER_RPC", Subject: "bagel.rpc.admin.deploy.>", Token: badToken}},
	}}

	activations, err := mintActivations(acl, mat, existing)
	if err != nil {
		t.Fatal(err)
	}
	reissued := findActivationToken(activations, "ADMIN_RPC", "DEPLOYER_RPC")
	if reissued == badToken {
		t.Fatal("reused a token that was not signed by the exporter's current key")
	}
	assertActivation(t, mat, "ADMIN_RPC", natsacl.Activation{From: "DEPLOYER_RPC", Subject: "bagel.rpc.admin.deploy.>", Token: reissued})
}

func TestMintActivationsSkipsUngatedExports(t *testing.T) {
	acl := &natsacl.ACL{Accounts: map[string]natsacl.AccountSpec{
		"EXPORTER": {Exports: []natsacl.ExportSpec{{Service: "svc.public.>"}}},
	}}
	mat := testMaterialFor(t, acl)

	activations, err := mintActivations(acl, mat, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(activations) != 0 {
		t.Fatalf("activations = %+v, want none for an ungated export", activations)
	}
}
