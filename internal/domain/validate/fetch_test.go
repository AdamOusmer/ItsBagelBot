// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package validate

import (
	"errors"
	"strings"
	"testing"

	"ItsBagelBot/internal/moderation"
)

func init() {
	CheckFloor = moderation.CheckFloor
}

func TestFetchDefName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"simple", "weather", nil},
		{"digits and underscore", "top_10_games", nil},
		{"single char", "a", nil},
		{"max 32", strings.Repeat("a", 32), nil},
		{"empty", "", ErrFetchDefName},
		{"33 chars", strings.Repeat("a", 33), ErrFetchDefName},
		{"upper case", "Weather", ErrFetchDefName},
		{"hyphen", "top-games", ErrFetchDefName},
		{"space", "top games", ErrFetchDefName},
		{"colon would forge a hash field", "wea:ther", ErrFetchDefName},
		{"dot breaks the token grammar", "wx.today", ErrFetchDefName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := FetchDefName(tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("FetchDefName(%q) = %v, want %v", tc.in, err, tc.want)
			}
		})
	}
}

func TestFetchDefNameFloor(t *testing.T) {
	slur := floorSlur(t)
	if err := FetchDefName("chat_" + leetify(slur)); !errors.Is(err, ErrContentFloor) {
		t.Fatalf("obfuscated slur in a def name must hit the floor, got %v", err)
	}
}

func TestFetchURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"plain https", "https://api.example.com/v1/weather?q=berlin", nil},
		{"signed query string at the cap", "https://s3.example.com/b?" + strings.Repeat("a", 487), nil},

		{"empty", "", ErrFetchURL},
		{"over 512", "https://api.example.com/" + strings.Repeat("p/", 300), ErrFetchURL},
		{"http refused", "http://api.example.com/v1", ErrFetchURL},
		{"ftp refused", "ftp://api.example.com/file", ErrFetchURL},
		{"schemeless refused", "api.example.com/v1", ErrFetchURL},
		{"opaque refused", "https:nohost.example", ErrFetchURL},
		{"no host refused", "https:///path", ErrFetchURL},
		{"unparseable", "https://exa mple.com", ErrFetchURL},

		// Host denylist half.
		{"ip literal v4", "https://127.0.0.1/admin", ErrFetchHost},
		{"metadata ip literal", "https://169.254.169.254/latest/meta-data", ErrFetchHost},
		{"ip literal v6", "https://[::1]/admin", ErrFetchHost},
		{"localhost", "https://localhost/api", ErrFetchHost},
		{"local suffix", "https://printer.local/api", ErrFetchHost},
		{"internal suffix", "https://nats.internal/api", ErrFetchHost},
		{"trailing dot forms normalized", "https://printer.local./api", ErrFetchHost},
		{"port does not hide the host", "https://localhost:8443/api", ErrFetchHost},

		// Immovable IP-logger floor.
		{"grabber host", "https://grabify.link/XYZ", ErrContentFloor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := FetchURL(tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("FetchURL(%q) = %v, want %v", tc.in, err, tc.want)
			}
		})
	}
}

func TestFetchHostAllowed(t *testing.T) {
	for host, want := range map[string]error{
		"api.openweathermap.org": nil,
		"API.EXAMPLE.COM.":       nil, // case + trailing dot normalize away
		"127.0.0.1":              ErrFetchHost,
		"::ffff:127.0.0.1":       ErrFetchHost,
		"":                       ErrFetchHost,
		"localhost":              ErrFetchHost,
		"myhost.local":           ErrFetchHost,
		"svc.internal":           ErrFetchHost,
	} {
		if err := FetchHostAllowed(host); !errors.Is(err, want) {
			t.Errorf("FetchHostAllowed(%q) = %v, want %v", host, err, want)
		}
	}
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
		{"depth 9 rejected", strings.Split("a.b.c.d.e.f.g.h.i", "."), ErrFetchPath},
		{"empty segment", []string{"data", ""}, ErrFetchPath},
		{"dot in segment", []string{"data.items"}, ErrFetchPath},
		{"dollar prefix", []string{"$data"}, ErrFetchPath},
		{"unicode", []string{"donnée"}, ErrFetchPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := FetchPath(tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("FetchPath(%v) = %v, want %v", tc.in, err, tc.want)
			}
		})
	}
}

func TestKeyLabelAndValue(t *testing.T) {
	labelCases := []struct {
		name string
		in   string
		want error
	}{
		{"plain label", "openweather", nil},
		{"max 32", strings.Repeat("k", 32), nil},
		{"empty", "", ErrKeyLabel},
		{"33 chars", strings.Repeat("k", 33), ErrKeyLabel},
		{"control char", "open\tweather", ErrKeyLabel},
		{"newline smuggled", "open\nweather", ErrKeyLabel},
	}
	for _, tc := range labelCases {
		t.Run("label/"+tc.name, func(t *testing.T) {
			if err := KeyLabel(tc.in); !errors.Is(err, tc.want) {
				t.Fatalf("KeyLabel(%q) = %v, want %v", tc.in, err, tc.want)
			}
		})
	}

	valueCases := []struct {
		name string
		in   string
		want error
	}{
		{"typical key", "sk-live-abc123DEF456", nil},
		{"max 512", strings.Repeat("x", 512), nil},
		{"empty", "", ErrKeyValue},
		{"513 chars", strings.Repeat("x", 513), ErrKeyValue},
	}
	for _, tc := range valueCases {
		t.Run("value/"+tc.name, func(t *testing.T) {
			if err := KeyValue(tc.in); !errors.Is(err, tc.want) {
				t.Fatalf("KeyValue(len=%d) = %v, want %v", len(tc.in), err, tc.want)
			}
		})
	}
}
