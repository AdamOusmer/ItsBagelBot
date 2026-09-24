// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"maps"
	"slices"
	"testing"

	"ItsBagelBot/internal/domain/rpc/deploy"
)

var deploySurface = map[string]string{
	"service": deploy.Prefix + ".>",
	"stream":  deploy.EventsPrefix + ".>",
}

func TestDeployerSurfaceIsPrivateToAdmin(t *testing.T) {
	catalog := loadRPCCatalog(t)
	private := make(map[string][]string)
	for _, exp := range catalog.accounts["DEPLOYER_RPC"].exports {
		if exp.accounts != nil {
			private[exp.kind+" "+exp.subject] = exp.accounts
		}
	}
	want := make(map[string][]string, len(deploySurface))
	for kind, subject := range deploySurface {
		want[kind+" "+subject] = []string{"ADMIN_RPC"}
	}
	if !maps.EqualFunc(private, want, slices.Equal[[]string]) {
		t.Fatalf("DEPLOYER_RPC private exports differ:\nwant %v\n got %v", want, private)
	}
}

func TestOnlyAdminImportsDeploySurface(t *testing.T) {
	var got []string
	for name, account := range loadRPCCatalog(t).accounts {
		got = append(got, deployImports(name, account)...)
	}
	slices.Sort(got)
	want := []string{
		"ADMIN_RPC service " + deploySurface["service"],
		"ADMIN_RPC stream " + deploySurface["stream"],
	}
	if !slices.Equal(got, want) {
		t.Fatalf("deploy surface importers differ:\nwant %v\n got %v", want, got)
	}
}

func deployImports(name string, account rpcAccount) []string {
	var out []string
	for _, imp := range account.imports {
		if imp.account == "DEPLOYER_RPC" && imp.subject == deploySurface[imp.kind] {
			out = append(out, name+" "+imp.kind+" "+imp.subject)
		}
	}
	return out
}
