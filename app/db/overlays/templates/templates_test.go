// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package templates_test

import (
	"testing"

	"ItsBagelBot/app/db/overlays/templates"

	"github.com/stretchr/testify/require"
)

func TestStarterDocumentsPassValidation(t *testing.T) {
	keys := templates.Keys()
	require.Len(t, keys, 10)
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			doc, found, err := templates.Lookup(key)

			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, key, doc.ID)
			require.NotEmpty(t, doc.Widgets)
		})
	}
}

func TestUnknownStarterIsNotFound(t *testing.T) {
	_, found, err := templates.Lookup("../main")

	require.NoError(t, err)
	require.False(t, found)
}
