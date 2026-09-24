// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"maps"
	"regexp"
	"strings"
	"testing"
)

var (
	hubDomainPattern = regexp.MustCompile(`(?m)^\s*domain:\s*(\S+)\s*$`)
	busBlockPattern  = regexp.MustCompile(`(?s)\n  BUS: \{\n(.*?)\n    users: \[`)
	mappingsPattern  = regexp.MustCompile(`(?s)mappings: \{\n(.*?)\n    \}`)
	mappingPattern   = regexp.MustCompile(`"([^"]+)": "([^"]+)"`)
)

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
	bus := busBlockPattern.FindStringSubmatch(sourceFile{name: "nats-auth.conf"}.read(t))
	if bus == nil {
		t.Fatal("nats-auth.conf has no BUS account header before its users")
	}
	block := mappingsPattern.FindStringSubmatch(bus[1])
	if block == nil {
		t.Fatal("BUS declares no mappings; a config reload refuses hub-domain JetStream calls until the server re-adds its own")
	}
	got := map[string]string{}
	for _, m := range mappingPattern.FindAllStringSubmatch(block[1], -1) {
		got[m[1]] = m[2]
	}
	if want := domainMappings(strings.Trim(domain[1], `"`)); !maps.Equal(got, want) {
		t.Fatalf("BUS mappings = %v, want the full %s domain table %v", got, domain[1], want)
	}
}
