// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

var preloadHeader = "# Copyright (c) 2026 Adam Ousmer. All rights reserved.\n# Proprietary. No license granted. See LICENSE.md.\n\n"

var preloadEntry = regexp.MustCompile(`(?m)^\s*(A[A-Z2-7]{55}):\s*"([^"]+)"`)

// writePreload renders every compiled account as a signed resolver_preload
// entry. A file whose claims already match is left untouched: re-signing
// changes every token, and git would see a diff on each run.
func writePreload(path string, acl *natsacl.ACL, keys *natsacl.Keys, signer nkeys.KeyPair) error {
	compiled, err := natsacl.Compile(acl, keys)
	if err != nil {
		return err
	}
	current, err := preloadCurrent(path, compiled, signer)
	if err != nil || current {
		return err
	}
	body, err := renderPreload(compiled, signer)
	if err != nil {
		return err
	}
	return writeIfChanged(path, []byte(body))
}

func renderPreload(compiled []*jwt.AccountClaims, signer nkeys.KeyPair) (string, error) {
	var b strings.Builder
	b.WriteString(preloadHeader)
	b.WriteString("resolver_preload: {\n")
	for _, c := range compiled {
		token, err := c.Encode(signer)
		if err != nil {
			return "", fmt.Errorf("natscreds: sign account %s: %w", c.Name, err)
		}
		fmt.Fprintf(&b, "  %s: %q\n", c.Subject, token)
	}
	b.WriteString("}\n")
	return b.String(), nil
}

func preloadCurrent(path string, compiled []*jwt.AccountClaims, signer nkeys.KeyPair) (bool, error) {
	existing, err := readPreload(path)
	if err != nil || len(existing) != len(compiled) {
		return false, nil
	}
	for _, c := range compiled {
		want, err := canonicalClaims(c, signer)
		if err != nil {
			return false, err
		}
		if !natsacl.Equivalent(existing[c.Subject], want) {
			return false, nil
		}
	}
	return true, nil
}

func readPreload(path string) (map[string]*jwt.AccountClaims, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*jwt.AccountClaims)
	for _, m := range preloadEntry.FindAllStringSubmatch(string(data), -1) {
		claims, err := jwt.DecodeAccountClaims(m[2])
		if err != nil {
			return nil, err
		}
		out[m[1]] = claims
	}
	return out, nil
}

// canonicalClaims gives compiled claims the same sign-and-decode trip a
// parsed token made, so Equivalent compares like with like.
func canonicalClaims(c *jwt.AccountClaims, signer nkeys.KeyPair) (*jwt.AccountClaims, error) {
	token, err := c.Encode(signer)
	if err != nil {
		return nil, err
	}
	return jwt.DecodeAccountClaims(token)
}
