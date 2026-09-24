// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package i18n

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func nonDefaultLocales(codes []string) []string {
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		if c != DefaultLocale {
			out = append(out, c)
		}
	}
	return out
}

func TestGapsExcludeDefaultLocale(t *testing.T) {
	if _, ok := Gaps()[DefaultLocale]; ok {
		t.Errorf("Gaps must exclude the default locale %q", DefaultLocale)
	}
}

func TestGapsKeyedByManifest(t *testing.T) {
	got := sortedKeys(Gaps())
	want := nonDefaultLocales(List())
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Gaps keys = %v, want List() minus %q = %v", got, DefaultLocale, want)
	}
}

func TestGapsAgreeWithMissing(t *testing.T) {
	for locale, missing := range Gaps() {
		if m := Missing(locale); !reflect.DeepEqual(missing, m) {
			t.Errorf("Gaps[%q] = %v, but Missing(%q) = %v", locale, missing, locale, m)
		}
	}
}

func TestGapsShowCompleteLocaleAsComplete(t *testing.T) {
	if n := len(Gaps()["fr"]); n != 0 {
		t.Errorf("fr is fully translated but Gaps reports %d missing key(s): %v", n, Gaps()["fr"])
	}
}

func TestSupported(t *testing.T) {
	cases := map[string]bool{"en": true, "fr": true, "xx": false, "": false}
	for code, want := range cases {
		if got := Supported(code); got != want {
			t.Errorf("Supported(%q) = %v, want %v", code, got, want)
		}
	}
}

func TestListSorted(t *testing.T) {
	got := List()
	if !sort.StringsAreSorted(got) {
		t.Errorf("List() is not sorted: %v", got)
	}
	if len(got) == 0 {
		t.Fatal("List() is unexpectedly empty")
	}
	if !Supported(DefaultLocale) {
		t.Errorf("List()/manifest must include the default locale %q: %v", DefaultLocale, got)
	}
}

func TestDashboardTokenRemoved(t *testing.T) {
	for locale := range catalog {
		for key, val := range catalog[locale] {
			if strings.Contains(val, dashboardToken) {
				t.Errorf("catalog[%q][%q] still carries the raw token %q", locale, key, dashboardToken)
			}
		}
	}
}

func TestDashboardURLExpanded(t *testing.T) {
	urlKeys := []string{KeyReauthRevokedBody, KeyReauthRevokedChat, KeyGrantDeadChat, KeyBotBannedBody}
	for _, locale := range Locales() {
		for _, key := range urlKeys {
			if !strings.Contains(T(locale, key), DashboardURL) {
				t.Errorf("T(%q, %q) is missing DashboardURL after expansion", locale, key)
			}
		}
	}
}

func TestSharedKeysResolve(t *testing.T) {
	keys := []string{
		KeyReauthRevokedTitle, KeyReauthRevokedBody, KeyReauthRevokedChat,
		KeyGrantDeadTitle, KeyGrantDeadBody, KeyGrantDeadChat,
		KeyBotBannedTitle, KeyBotBannedBody,
	}

	for _, locale := range Locales() {
		for _, key := range keys {
			got := T(locale, key)
			if got == key {
				t.Errorf("T(%q, %q) fell through to the key itself", locale, key)
			}
			if got == "" {
				t.Errorf("T(%q, %q) is empty", locale, key)
			}
		}
	}
}

func TestGrantDeadCopyAvoidsRevocationBlame(t *testing.T) {
	blame := map[string][]string{
		"en": {"revoked", "password"},
		"fr": {"révoqué", "mot de passe"},
	}

	for locale, terms := range blame {
		for _, key := range []string{KeyGrantDeadTitle, KeyGrantDeadBody, KeyGrantDeadChat} {
			body := T(locale, key)
			for _, term := range terms {
				if strings.Contains(body, term) {
					t.Errorf("T(%q, %q) misattributes cause: contains %q", locale, key, term)
				}
			}
		}
	}
}

func TestFallbackChain(t *testing.T) {
	if got, want := T("fr", KeyGrantDeadTitle), T(DefaultLocale, KeyGrantDeadTitle); got == want {
		t.Errorf("fr and en copy for %q are identical, so the fr entry is not being used", KeyGrantDeadTitle)
	}
	if got, want := T("zz", KeyGrantDeadTitle), T(DefaultLocale, KeyGrantDeadTitle); got != want {
		t.Errorf("unknown locale did not fall back to %q: got %q", DefaultLocale, got)
	}
	if got := T(DefaultLocale, "nope.not.a.key"); got != "nope.not.a.key" {
		t.Errorf("unknown key should return itself, got %q", got)
	}
}
