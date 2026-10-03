// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package validate_test

import (
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/validate"

	"github.com/stretchr/testify/assert"
)

type fetchRule struct {
	name string
	in   string
	want error
}

func assertRule(t *testing.T, rule func(string) error, cases []fetchRule) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := rule(tc.in)
			if tc.want == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestFetchDefName(t *testing.T) {
	assertRule(t, validate.FetchDefName, []fetchRule{
		{"simple", "weather", nil},
		{"digits and underscore", "top_10_games", nil},
		{"single char", "a", nil},
		{"max 32", strings.Repeat("a", 32), nil},
		{"empty", "", validate.ErrFetchDefName},
		{"33 chars", strings.Repeat("a", 33), validate.ErrFetchDefName},
		{"upper case", "Weather", validate.ErrFetchDefName},
		{"hyphen", "top-games", validate.ErrFetchDefName},
		{"space", "top games", validate.ErrFetchDefName},
		{"colon would forge a hash field", "wea:ther", validate.ErrFetchDefName},
		{"dot breaks the token grammar", "wx.today", validate.ErrFetchDefName},
	})
}

func TestFetchURL(t *testing.T) {
	assertRule(t, validate.FetchURL, []fetchRule{
		{"plain https", "https://api.example.com/v1/weather?q=berlin", nil},
		{"signed query string at the cap", "https://s3.example.com/b?" + strings.Repeat("a", 487), nil},

		{"empty", "", validate.ErrFetchURL},
		{"over 512", "https://api.example.com/" + strings.Repeat("p/", 300), validate.ErrFetchURL},
		{"http refused", "http://api.example.com/v1", validate.ErrFetchURL},
		{"ftp refused", "ftp://api.example.com/file", validate.ErrFetchURL},
		{"schemeless refused", "api.example.com/v1", validate.ErrFetchURL},
		{"opaque refused", "https:nohost.example", validate.ErrFetchURL},
		{"no host refused", "https:///path", validate.ErrFetchURL},
		{"unparseable", "https://exa mple.com", validate.ErrFetchURL},

		{"ip literal v4", "https://127.0.0.1/admin", validate.ErrFetchHost},
		{"metadata ip literal", "https://169.254.169.254/latest/meta-data", validate.ErrFetchHost},
		{"ip literal v6", "https://[::1]/admin", validate.ErrFetchHost},
		{"localhost", "https://localhost/api", validate.ErrFetchHost},
		{"local suffix", "https://printer.local/api", validate.ErrFetchHost},
		{"internal suffix", "https://nats.internal/api", validate.ErrFetchHost},
		{"trailing dot forms normalized", "https://printer.local./api", validate.ErrFetchHost},
		{"port does not hide the host", "https://localhost:8443/api", validate.ErrFetchHost},

		{"grabber host", "https://grabify.link/XYZ", validate.ErrContentFloor},
	})
}

func TestFetchHostAllowed(t *testing.T) {
	assertRule(t, validate.FetchHostAllowed, []fetchRule{
		{"public host", "api.openweathermap.org", nil},
		{"case and trailing dot are normalized", "API.EXAMPLE.COM.", nil},
		{"ipv4 literal", "127.0.0.1", validate.ErrFetchHost},
		{"ipv4 mapped in ipv6", "::ffff:127.0.0.1", validate.ErrFetchHost},
		{"empty", "", validate.ErrFetchHost},
		{"localhost", "localhost", validate.ErrFetchHost},
		{"local suffix", "myhost.local", validate.ErrFetchHost},
		{"internal suffix", "svc.internal", validate.ErrFetchHost},
	})
}

func TestFetchPath(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want error
	}{
		{"nil is plain kind", nil, nil},
		{"dotted path", []string{"data", "items", "0", "name"}, nil},
		{"indices as bare digits", []string{"forecast", "2"}, nil},
		{"hyphen and underscore", []string{"current_conditions", "feels-like"}, nil},
		{"depth 8 ok", strings.Split("a.b.c.d.e.f.g.h", "."), nil},
		{"depth 9 rejected", strings.Split("a.b.c.d.e.f.g.h.i", "."), validate.ErrFetchPath},
		{"empty segment", []string{"data", ""}, validate.ErrFetchPath},
		{"dot in segment", []string{"data.items"}, validate.ErrFetchPath},
		{"dollar prefix", []string{"$data"}, validate.ErrFetchPath},
		{"unicode", []string{"donnée"}, validate.ErrFetchPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validate.FetchPath(tc.in)
			if tc.want == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestKeyLabel(t *testing.T) {
	assertRule(t, validate.KeyLabel, []fetchRule{
		{"plain label", "openweather", nil},
		{"max 32", strings.Repeat("k", 32), nil},
		{"empty", "", validate.ErrKeyLabel},
		{"33 chars", strings.Repeat("k", 33), validate.ErrKeyLabel},
		{"control char", "open\tweather", validate.ErrKeyLabel},
		{"newline smuggled", "open\nweather", validate.ErrKeyLabel},
	})
}

func TestKeyValue(t *testing.T) {
	assertRule(t, validate.KeyValue, []fetchRule{
		{"typical key shape accepted", "live credential sample for length check", nil},
		{"max 512", strings.Repeat("x", 512), nil},
		{"empty", "", validate.ErrKeyValue},
		{"513 chars", strings.Repeat("x", 513), validate.ErrKeyValue},
	})
}
