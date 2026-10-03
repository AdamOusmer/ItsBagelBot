// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	pinnedAccounts = regexp.MustCompile(`(?m)^\s*accounts:\s*\["([^"]+)"\]`)
	preloadToken   = regexp.MustCompile(`(?m)^\s*(A[A-Z2-7]{55}):\s*"([^"]+)"`)
	hubDomain      = regexp.MustCompile(`(?m)^\s*domain:\s*(\S+)\s*$`)
)

func committedKeys(t *testing.T) *natsacl.Keys {
	t.Helper()
	keys, err := natsacl.LoadKeys("accounts.keys.yaml")
	require.NoError(t, err)
	return keys
}

func committedOperator(t *testing.T) *jwt.OperatorClaims {
	t.Helper()
	operator, err := jwt.DecodeOperatorClaims(strings.TrimSpace(readText(t, "operator.jwt")))
	require.NoError(t, err)
	return operator
}

func committedPreload(t *testing.T) map[string]*jwt.AccountClaims {
	t.Helper()
	preloaded := map[string]*jwt.AccountClaims{}
	for _, m := range preloadToken.FindAllStringSubmatch(readText(t, "nats-accounts.conf"), -1) {
		claims, err := jwt.DecodeAccountClaims(m[2])
		require.NoError(t, err, "preload entry %s does not decode", m[1])
		require.Equal(t, m[1], claims.Subject, "preload entry %s does not decode to its own account", m[1])
		preloaded[m[1]] = claims
	}
	return preloaded
}

func hubBlock(t *testing.T, name string) string {
	t.Helper()
	block := regexp.MustCompile(`(?s)` + name + ` \{.*?\n\}`).FindString(readText(t, "nats-server.conf"))
	require.NotEmpty(t, block, "nats-server.conf has no %s block", name)
	return block
}

func TestHubPinsTheBusAccountByPublicKey(t *testing.T) {
	bus := committedKeys(t).Accounts["BUS"]

	pinned := pinnedAccounts.FindStringSubmatch(readText(t, "nats-server.conf"))

	require.NotNil(t, pinned, "hub cluster pins no account")
	assert.Equal(t, bus, pinned[1], "hub cluster must pin the BUS public key")
}

func TestBothClustersMountTheOperatorTrustFiles(t *testing.T) {
	kustomization := readText(t, "kustomization.yaml")
	for _, entry := range []string{"auth.conf=nats-operator.conf", "- operator.jwt", "accounts.conf=nats-accounts.conf"} {
		assert.Equal(t, 2, strings.Count(kustomization, entry), "%q must appear in both the hub and leaf config maps", entry)
	}
}

func TestPreloadCoversEveryAccountSignedByTheOperator(t *testing.T) {
	operator, preloaded := committedOperator(t), committedPreload(t)
	for name, pub := range committedKeys(t).Accounts {
		claims := preloaded[pub]
		assert.True(t, claims != nil && operator.DidSign(claims), "account %s (%s) is not preloaded under this operator", name, pub)
	}
}

func TestPreloadedDeadLetterOwnersHaveRuntimePermissions(t *testing.T) {
	bus := committedPreload(t)[committedKeys(t).Accounts["BUS"]]
	for _, role := range []string{"projector_bus", "users_bus"} {
		t.Run(role, func(t *testing.T) {
			scope, ok := bus.SigningKeys[committedKeys(t).Roles["BUS"][role]].(*jwt.UserScope)
			require.True(t, ok, "missing scoped signer for %s", role)

			for _, action := range []string{"INFO", "CREATE", "UPDATE"} {
				want := "$JS.API.STREAM." + action + ".BAGEL_DLQ"
				assert.True(t, slices.Contains(scope.Template.Pub.Allow, want), "preload %s lacks %s", role, want)
			}
		})
	}
}

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
	domain := hubDomain.FindStringSubmatch(readText(t, "nats-server.conf"))
	require.NotNil(t, domain, "nats-server.conf declares no JetStream domain")
	compiled, err := natsacl.Compile(committedACL(t), committedKeys(t))
	require.NoError(t, err)
	index := slices.IndexFunc(compiled, func(claims *jwt.AccountClaims) bool { return claims.Name == "BUS" })
	require.GreaterOrEqual(t, index, 0, "accounts.yaml has no BUS account")

	got := map[string]string{}
	for subject, targets := range compiled[index].Mappings {
		for _, target := range targets {
			got[string(subject)] = string(target.Subject)
		}
	}

	assert.Equal(t, domainMappings(strings.Trim(domain[1], `"`)), got,
		"without the full %s domain table an account update refuses hub-domain JetStream calls until the server re-adds its own", domain[1])
}

func TestHubRoutesRetainMeasuredCompressionMode(t *testing.T) {
	cluster := hubBlock(t, "cluster")

	assert.Regexp(t, `(?m)^\s*mode:\s*s2_fast\s*$`, cluster, "BUS routes must retain the measured s2_fast compression mode")
	assert.NotRegexp(t, `(?m)^\s*(mode:\s*s2_auto\s*$|rtt_thresholds:)`, cluster, "adaptive route compression regressed the asymmetric R3 topology")
}

func TestHubJetStreamIngestWindowStaysByteBounded(t *testing.T) {
	jetstream := hubBlock(t, "jetstream")

	for _, required := range []string{"max_buffered_msgs: 262144", "max_buffered_size: 128MB"} {
		assert.Contains(t, jetstream, required, "JetStream ingest guard is missing")
	}
}

func TestPodNetworkOnly(t *testing.T) {
	for _, name := range []string{"nats.yaml", "nats-leaf.yaml"} {
		assert.NotContains(t, readText(t, name), "hostNetwork", "%s must not set hostNetwork: members advertise headless FQDNs on the pod network", name)
	}
}

func TestLeafServicesKeepPublishedClientPorts(t *testing.T) {
	manifest := readText(t, "nats-leaf.yaml")
	published := map[string]int{
		"- {name: client, port: 4222, targetPort: client}":   2,
		"- {name: monitor, port: 8222, targetPort: monitor}": 2,
	}
	for entry, want := range published {
		assert.Equal(t, want, strings.Count(manifest, entry), "%q must appear in nats-leaf + nats-leaf-local", entry)
	}
}
