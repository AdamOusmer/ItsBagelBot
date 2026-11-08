// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"fmt"
	"os"
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/conf"
	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/require"
)

// Compare the actual signed resolver claims, not the encoded JWT text. Re-signing
// changes timestamps and signatures without changing the effective ACL.
func TestCommittedPreloadMatchesCompiledACL(t *testing.T) {
	keys, err := natsacl.LoadKeys("accounts.keys.yaml")
	require.NoError(t, err)
	compiled, err := natsacl.Compile(committedACL(t), keys)
	require.NoError(t, err)

	body, err := os.ReadFile("nats-accounts.conf")
	require.NoError(t, err)
	require.NoError(t, comparePreload(string(body), compiled))

	t.Run("rejects an extra preloaded account", func(t *testing.T) {
		require.ErrorContains(t, comparePreload(string(body), compiled[:len(compiled)-1]), "accounts")
	})
	t.Run("rejects changed claims", func(t *testing.T) {
		changed := append([]*jwt.AccountClaims(nil), compiled...)
		modified := *changed[0]
		modified.Exports = append(append(jwt.Exports{}, modified.Exports...), &jwt.Export{Subject: "test.extra.>", Type: jwt.Service})
		changed[0] = &modified
		require.ErrorContains(t, comparePreload(string(body), changed), "differs from compiled ACL")
	})
}

func comparePreload(body string, compiled []*jwt.AccountClaims) error {
	entries, err := parsePreloadMap(body)
	if err != nil {
		return err
	}
	if len(entries) != len(compiled) {
		return fmt.Errorf("preload has %d accounts; compiled ACL has %d", len(entries), len(compiled))
	}

	signer, err := nkeys.CreateOperator()
	if err != nil {
		return err
	}
	defer signer.Wipe()
	for _, account := range compiled {
		if err := comparePreloadedAccount(entries, account, signer); err != nil {
			return err
		}
	}
	return nil
}

func parsePreloadMap(body string) (map[string]any, error) {
	config, err := conf.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse preload: %w", err)
	}
	entries, ok := config["resolver_preload"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing resolver_preload map")
	}
	return entries, nil
}

func comparePreloadedAccount(entries map[string]any, account *jwt.AccountClaims, signer nkeys.KeyPair) error {
	got, err := decodePreloadedAccount(entries, account)
	if err != nil {
		return err
	}
	want, err := canonicalAccount(account, signer)
	if err != nil {
		return err
	}
	if !natsacl.Equivalent(got, want) {
		return fmt.Errorf("preload account %s differs from compiled ACL", account.Name)
	}
	return nil
}

func decodePreloadedAccount(entries map[string]any, account *jwt.AccountClaims) (*jwt.AccountClaims, error) {
	raw, exists := entries[account.Subject]
	if !exists {
		return nil, fmt.Errorf("preload missing account %s", account.Name)
	}
	token, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("preload account %s is not a JWT string", account.Name)
	}
	got, err := jwt.DecodeAccountClaims(token)
	if err != nil {
		return nil, fmt.Errorf("decode account %s: %w", account.Name, err)
	}
	if got.Subject != account.Subject {
		return nil, fmt.Errorf("preload account %s subject does not match its key", account.Name)
	}
	return got, nil
}

// Match the generator's sign-and-decode round trip so empty collections have
// the same representation as they do in the committed JWTs.
func canonicalAccount(account *jwt.AccountClaims, signer nkeys.KeyPair) (*jwt.AccountClaims, error) {
	encoded, err := account.Encode(signer)
	if err != nil {
		return nil, fmt.Errorf("canonicalize account %s: %w", account.Name, err)
	}
	want, err := jwt.DecodeAccountClaims(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode compiled account %s: %w", account.Name, err)
	}
	return want, nil
}
