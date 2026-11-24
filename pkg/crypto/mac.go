// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package crypto

import (
	"github.com/tink-crypto/tink-go/v2/mac"
	"github.com/tink-crypto/tink-go/v2/tink"
	"go.uber.org/zap"
)

func OptionalMACFromEnv(pathEnv KeysetPathEnv, feature string, log *zap.Logger) tink.MAC {
	keysetJSON := optionalKeyset(pathEnv, feature, log)
	if keysetJSON == nil {
		return nil
	}
	handle, err := readKeyset(keysetJSON)
	if err != nil {
		log.Fatal("invalid tink MAC keyset", zap.String("feature", feature), zap.Error(err))
	}
	primitive, err := mac.New(handle)
	if err != nil {
		log.Fatal("failed to initialize tink MAC", zap.String("feature", feature), zap.Error(err))
	}
	return primitive
}
