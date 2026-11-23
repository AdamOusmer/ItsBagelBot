// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package consent_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/consent"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

func TestTermsVersionMatchesLegalContent(t *testing.T) {
	dates := []time.Time{legalUpdated(t, "terms"), legalUpdated(t, "privacy")}
	latest := slices.MaxFunc(dates, time.Time.Compare)
	require.Equal(t, latest.Format("2006-01"), consent.TermsVersion)
}

func legalUpdated(t *testing.T, policy string) time.Time {
	t.Helper()
	path := filepath.Join("..", "..", "..", "web", "marketing", "src", "content", "legal", policy, "en", "meta.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	value, err := codec.ExtractString(data, codec.Path{"updated"})
	require.NoError(t, err, "%s", path)
	updated, err := time.Parse("January 2006", value)
	require.NoError(t, err, "%s", path)
	return updated
}
