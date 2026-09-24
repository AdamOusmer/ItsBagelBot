// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
)

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

	activations, err := mintActivations(acl, mat)
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

func TestMintActivationsSkipsUngatedExports(t *testing.T) {
	acl := &natsacl.ACL{Accounts: map[string]natsacl.AccountSpec{
		"EXPORTER": {Exports: []natsacl.ExportSpec{{Service: "svc.public.>"}}},
	}}
	mat := testMaterialFor(t, acl)

	activations, err := mintActivations(acl, mat)
	if err != nil {
		t.Fatal(err)
	}
	if len(activations) != 0 {
		t.Fatalf("activations = %+v, want none for an ungated export", activations)
	}
}
