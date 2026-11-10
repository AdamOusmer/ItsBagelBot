// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package crypto

import (
	"errors"
	"os"

	"go.uber.org/zap"

	"ItsBagelBot/pkg/env"
)

const keysetPathEnv = "TINK_KEYSET_PATH"

func MustFromEnv(log *zap.Logger) *Crypto {
	return mustLoad(env.MustGet(keysetPathEnv), log)
}

func OptionalFromEnv(feature string, log *zap.Logger) *Crypto {
	path := env.Get(keysetPathEnv, "")
	if path == "" {
		log.Warn(feature + " key custody disabled: TINK_KEYSET_PATH not set")
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		log.Warn(feature+" key custody disabled: keyset not provisioned yet", zap.String("path", path))
		return nil
	}
	return mustLoad(path, log)
}

func mustLoad(path string, log *zap.Logger) *Crypto {
	keysetJSON, err := os.ReadFile(path)
	if err != nil {
		log.Fatal("failed to read tink keyset", zap.Error(err))
	}
	packer, err := NewCrypto(keysetJSON)
	if err != nil {
		log.Fatal("failed to initialize crypto", zap.Error(err))
	}
	return packer
}
