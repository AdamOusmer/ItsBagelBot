// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"crypto/rand"
	"math/big"
)

func cryptoInt(bound int64) (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(bound))
	if err != nil {
		return 0, err
	}
	return n.Int64(), nil
}

func mustCryptoInt(scope string, bound int64) int64 {
	n, err := cryptoInt(bound)
	if err != nil {
		panic(scope + ": crypto/rand unavailable: " + err.Error())
	}
	return n
}
