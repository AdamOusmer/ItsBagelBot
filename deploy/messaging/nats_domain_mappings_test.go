// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"maps"
	"regexp"
	"strings"
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
)

var hubDomainPattern = regexp.MustCompile(`(?m)^\s*domain:\s*(\S+)\s*$`)

// The server's own table for a JetStream domain (generateJSMappingTable in nats-server).
func domainMappings(domain string) map[string]string {
	prefix := "$JS." + domain + ".API."
	return map[string]string{
		prefix + "INFO":       "$JS.API.INFO",
		prefix + "STREAM.>":   "$JS.API.STREAM.>",
		prefix + "CONSUMER.>": "$JS.API.CONSUMER.>",
		prefix + "DIRECT.>":   "$JS.API.DIRECT.>",
		prefix + "META.>":     "$JS.API.META.>",
		prefix + "SERVER.>":   "$JS.API.SERVER.>",
		prefix + "ACCOUNT.>":  "$JS.API.ACCOUNT.>",
		prefix + "$KV.>":      "$KV.>",
		prefix + "$OBJ.>":     "$OBJ.>",
	}
}

func TestBusAccountDeclaresTheHubDomainMappings(t *testing.T) {
	server := sourceFile{name: "nats-server.conf"}.read(t)
	domain := hubDomainPattern.FindStringSubmatch(server)
	if domain == nil {
		t.Fatal("nats-server.conf declares no JetStream domain")
	}
	got := map[string]string{}
	for subject, targets := range busClaims(t).Mappings {
		for _, target := range targets {
			got[string(subject)] = string(target.Subject)
		}
	}
	if want := domainMappings(strings.Trim(domain[1], `"`)); !maps.Equal(got, want) {
		t.Fatalf("BUS mappings = %v, want the full %s domain table %v; without them an account update refuses hub-domain JetStream calls until the server re-adds its own", got, domain[1], want)
	}
}

func busClaims(t *testing.T) *jwt.AccountClaims {
	t.Helper()
	compiled, err := natsacl.Compile(committedACL(t), committedKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, claims := range compiled {
		if claims.Name == "BUS" {
			return claims
		}
	}
	t.Fatal("accounts.yaml has no BUS account")
	return nil
}
