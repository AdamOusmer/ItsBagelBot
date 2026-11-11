// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type syncedSecrets struct {
	Kind string `yaml:"kind"`
	Spec struct {
		Secrets []string `yaml:"secrets"`
	} `yaml:"spec"`
}

func TestAdminReceivesOnlyScopedRuntimeCredentials(t *testing.T) {
	docs := decodeFile[syncedSecrets](t, "admin.yaml")
	index := slices.IndexFunc(docs, func(doc syncedSecrets) bool { return doc.Kind == "DopplerSecret" })
	require.GreaterOrEqual(t, index, 0)
	keys := docs[index].Spec.Secrets
	require.NotEmpty(t, keys)
	assert.Subset(t, keys, []string{"DB_USER", "DB_PASS", "DB_SCHEMA", "NATS_RPC_JWT", "NATS_RPC_NKEY_SEED", "VALKEY_PASSWORD"})
	for _, forbidden := range []string{"USER", "PASSWORD", "NATS_JWT", "NATS_NKEY_SEED"} {
		assert.NotContains(t, keys, forbidden)
	}
}
