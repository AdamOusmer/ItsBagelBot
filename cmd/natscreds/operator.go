// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"fmt"

	"github.com/nats-io/jwt/v2"
)

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
