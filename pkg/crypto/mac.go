// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package crypto

import (
	"fmt"
	"os"

	"ItsBagelBot/pkg/env"

	"github.com/tink-crypto/tink-go/v2/mac"
	"github.com/tink-crypto/tink-go/v2/tink"
)

func OptionalMACFromEnv(pathEnv string) (tink.MAC, error) {
	path := env.Get(pathEnv, "")
	if path == "" {
		return nil, nil
	}
	keysetJSON, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s keyset: %w", pathEnv, err)
	}
	handle, err := readKeyset(keysetJSON)
	if err != nil {
		return nil, err
	}
	return mac.New(handle)
}
