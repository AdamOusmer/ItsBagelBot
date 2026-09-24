// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"regexp"
	"strings"
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
)

var (
	pinnedAccounts = regexp.MustCompile(`(?m)^\s*accounts:\s*\["([^"]+)"\]`)
	preloadToken   = regexp.MustCompile(`(?m)^\s*(A[A-Z2-7]{55}):\s*"([^"]+)"`)
)

func TestHubPinsTheBusAccountByPublicKey(t *testing.T) {
	bus := committedKeys(t).Accounts["BUS"]
	m := pinnedAccounts.FindStringSubmatch(sourceFile{name: "nats-server.conf"}.read(t))
	if m == nil || m[1] != bus {
		t.Fatalf("hub cluster pins %v, want BUS public key %s", m, bus)
	}
}

func TestBothClustersMountTheOperatorTrustFiles(t *testing.T) {
	kustomization := sourceFile{name: "kustomization.yaml"}.read(t)
	for _, entry := range []string{"auth.conf=nats-operator.conf", "- operator.jwt", "accounts.conf=nats-accounts.conf"} {
		if got := strings.Count(kustomization, entry); got != 2 {
			t.Errorf("%q appears in %d config maps, want both hub and leaf", entry, got)
		}
	}
}

func TestPreloadCoversEveryAccountSignedByTheOperator(t *testing.T) {
	operator := committedOperator(t)
	preloaded := committedPreload(t)
	for name, pub := range committedKeys(t).Accounts {
		claims := preloaded[pub]
		if claims == nil || !operator.DidSign(claims) {
			t.Errorf("account %s (%s) is not preloaded under this operator", name, pub)
		}
	}
}

func committedKeys(t *testing.T) *natsacl.Keys {
	t.Helper()
	keys, err := natsacl.LoadKeys("accounts.keys.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func committedOperator(t *testing.T) *jwt.OperatorClaims {
	t.Helper()
	operator, err := jwt.DecodeOperatorClaims(strings.TrimSpace(sourceFile{name: "operator.jwt"}.read(t)))
	if err != nil {
		t.Fatal(err)
	}
	return operator
}

// committedPreload keys each preloaded account by the public key its entry
// is filed under, refusing an entry whose claims name a different subject.
func committedPreload(t *testing.T) map[string]*jwt.AccountClaims {
	t.Helper()
	preloaded := map[string]*jwt.AccountClaims{}
	for _, m := range preloadToken.FindAllStringSubmatch(sourceFile{name: "nats-accounts.conf"}.read(t), -1) {
		claims, err := jwt.DecodeAccountClaims(m[2])
		if err != nil || claims.Subject != m[1] {
			t.Fatalf("preload entry %s does not decode to its own account: %v", m[1], err)
		}
		preloaded[m[1]] = claims
	}
	return preloaded
}
