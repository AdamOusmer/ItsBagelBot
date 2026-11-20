// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package crypto

import (
	"errors"
	"os"

	"go.uber.org/zap"

	"ItsBagelBot/pkg/env"
)

type KeysetPathEnv string

const keysetPathEnv KeysetPathEnv = "TINK_KEYSET_PATH"

func MustFromEnv(log *zap.Logger) *Crypto {
	path := env.MustGet(string(keysetPathEnv))
	keysetJSON, err := os.ReadFile(path)
	if err != nil {
		log.Fatal("failed to read tink keyset", zap.Error(err))
	}
	return mustCrypto(keysetJSON, log)
}

func OptionalFromEnv(feature string, log *zap.Logger) *Crypto {
	keysetJSON := optionalKeyset(keysetPathEnv, feature, log)
	if keysetJSON == nil {
		return nil
	}
	return mustCrypto(keysetJSON, log)
}

func optionalKeyset(pathEnv KeysetPathEnv, feature string, log *zap.Logger) []byte {
	name := string(pathEnv)
	path := env.Get(name, "")
	if path == "" {
		log.Warn(feature + " key custody disabled: " + name + " not set")
		return nil
	}
	keysetJSON, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		log.Warn(feature+" key custody disabled: keyset not provisioned yet", zap.String("path", path))
		return nil
	}
	if err != nil {
		log.Fatal("failed to read tink keyset", zap.String("feature", feature), zap.Error(err))
	}
	return keysetJSON
}

func mustCrypto(keysetJSON []byte, log *zap.Logger) *Crypto {
	packer, err := NewCrypto(keysetJSON)
	if err != nil {
		log.Fatal("failed to initialize crypto", zap.Error(err))
	}
	return packer
}
