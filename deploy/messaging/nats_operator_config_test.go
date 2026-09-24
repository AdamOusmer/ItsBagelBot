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
	keys, err := natsacl.LoadKeys("accounts.keys.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m := pinnedAccounts.FindStringSubmatch(sourceFile{name: "nats-server.conf"}.read(t))
	if m == nil || m[1] != keys.Accounts["BUS"] {
		t.Fatalf("hub cluster pins %v, want BUS public key %s", m, keys.Accounts["BUS"])
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
	keys, err := natsacl.LoadKeys("accounts.keys.yaml")
	if err != nil {
		t.Fatal(err)
	}
	operator, err := jwt.DecodeOperatorClaims(strings.TrimSpace(sourceFile{name: "operator.jwt"}.read(t)))
	if err != nil {
		t.Fatal(err)
	}
	preloaded := map[string]bool{}
	for _, m := range preloadToken.FindAllStringSubmatch(sourceFile{name: "nats-accounts.conf"}.read(t), -1) {
		claims, err := jwt.DecodeAccountClaims(m[2])
		if err != nil {
			t.Fatalf("preload %s: %v", m[1], err)
		}
		if claims.Subject != m[1] || !operator.DidSign(claims) {
			t.Fatalf("preload %s: subject %s issued by %s, not this operator", m[1], claims.Subject, claims.Issuer)
		}
		preloaded[m[1]] = true
	}
	for name, pub := range keys.Accounts {
		if !preloaded[pub] {
			t.Errorf("account %s (%s) missing from nats-accounts.conf", name, pub)
		}
	}
}
