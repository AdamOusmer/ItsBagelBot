// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/nats-io/jwt/v2"
)

// resolveOperatorJWT reuses the on-disk operator JWT verbatim when its
// claims already say what this run would mint, so an unchanged run leaves
// operator.jwt byte-identical; otherwise it mints a fresh one.
func resolveOperatorJWT(path string, mat *material, sysAccountPub string) (string, error) {
	if token, ok := reusableOperatorJWT(path, mat, sysAccountPub); ok {
		return token, nil
	}
	return mintOperatorJWT(mat, sysAccountPub)
}

func reusableOperatorJWT(path string, mat *material, sysAccountPub string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	token := strings.TrimSpace(string(raw))
	claims, err := jwt.DecodeOperatorClaims(token)
	if err != nil {
		return "", false
	}
	return token, operatorClaimsMatch(claims, mat, sysAccountPub)
}

func operatorClaimsMatch(claims *jwt.OperatorClaims, mat *material, sysAccountPub string) bool {
	operatorPub, err := mat.operator.PublicKey()
	if err != nil {
		return false
	}
	signingPub, err := mat.operatorSigning.PublicKey()
	if err != nil {
		return false
	}
	if claims.Subject != operatorPub || claims.SystemAccount != sysAccountPub {
		return false
	}
	return slices.Equal([]string(claims.SigningKeys), []string{signingPub})
}

// mintOperatorJWT builds the operator's self-signed JWT: it lists the one
// operator signing key that mints account claims, and names SYS as the
// system account nats-server needs for $SYS.> requests.
func mintOperatorJWT(mat *material, sysAccountPub string) (string, error) {
	operatorPub, err := mat.operator.PublicKey()
	if err != nil {
		return "", err
	}
	signingPub, err := mat.operatorSigning.PublicKey()
	if err != nil {
		return "", err
	}
	claims := jwt.NewOperatorClaims(operatorPub)
	claims.SigningKeys = jwt.StringList{signingPub}
	claims.SystemAccount = sysAccountPub
	token, err := claims.Encode(mat.operator)
	if err != nil {
		return "", fmt.Errorf("natscreds: encode operator jwt: %w", err)
	}
	return token, nil
}
