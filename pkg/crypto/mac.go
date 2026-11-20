// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package crypto

import (
	"errors"
	"os"

	"ItsBagelBot/pkg/env"

	"github.com/tink-crypto/tink-go/v2/mac"
	"github.com/tink-crypto/tink-go/v2/tink"
	"go.uber.org/zap"
)

const macKeysetPathEnv = "MARKETING_ADDRESS_HMAC_KEYSET_PATH"

func OptionalMACFromEnv(feature string, log *zap.Logger) tink.MAC {
	path := env.Get(macKeysetPathEnv, "")
	if path == "" {
		log.Warn(feature + " key custody disabled: " + macKeysetPathEnv + " not set")
		return nil
	}
	keysetJSON, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		log.Warn(feature+" key custody disabled: keyset not provisioned yet", zap.String("path", path))
		return nil
	}
	if err != nil {
		log.Fatal("failed to read tink MAC keyset", zap.String("feature", feature), zap.Error(err))
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
