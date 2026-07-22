package i18n

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The Gaps tests replace the old strict parity test. The locked product
// decision is that a key missing from a locale WARNS and falls back to English
// rather than failing the build, so a partially translated language can ship.
// They therefore verify the reporting mechanism instead of enforcing parity.

// sortedKeys returns the map's keys, sorted, never nil.
func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// nonDefaultLocales filters DefaultLocale out of codes, never returning nil.
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

// TestGapsAgreeWithMissing stands in for a fabricated-gap case, which embed
// makes awkward to inject: Gaps for a locale is exactly Missing for that locale.
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

// TestSupported checks the manifest-backed validation the users service relies
// on to reject a bogus locale before persisting it.
func TestSupported(t *testing.T) {
	cases := map[string]bool{"en": true, "fr": true, "xx": false, "": false}
	for code, want := range cases {
		if got := Supported(code); got != want {
			t.Errorf("Supported(%q) = %v, want %v", code, got, want)
		}
	}
}

// TestListSorted verifies List returns the manifest in sorted order and always
// carries the English source locale. It deliberately does not hardcode the full
// set: dropping in a new language must keep this test green with no Go edit.
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

// TestDashboardTokenRemoved proves parse-time expansion removed the raw
// placeholder from every loaded catalog value.
func TestDashboardTokenRemoved(t *testing.T) {
	for locale := range catalog {
		for key, val := range catalog[locale] {
			if strings.Contains(val, dashboardToken) {
				t.Errorf("catalog[%q][%q] still carries the raw token %q", locale, key, dashboardToken)
			}
		}
	}
}

// TestDashboardURLExpanded verifies every message that used to concatenate the
// dashboard URL now contains the real URL loaded from the catalog.
func TestDashboardURLExpanded(t *testing.T) {
	urlKeys := []string{KeyReauthRevokedBody, KeyReauthRevokedChat, KeyGrantDeadChat}
	for _, locale := range Locales() {
		for _, key := range urlKeys {
			if !strings.Contains(T(locale, key), DashboardURL) {
				t.Errorf("T(%q, %q) is missing DashboardURL after expansion", locale, key)
			}
		}
	}
}

// TestSharedKeysResolve guards the cross-service copy specifically. T falls back
// to returning the key itself, so a typo in one of these constants would post
// the literal string "grant.dead.chat" into a streamer's public chat.
func TestSharedKeysResolve(t *testing.T) {
	keys := []string{
		KeyReauthRevokedTitle, KeyReauthRevokedBody, KeyReauthRevokedChat,
		KeyGrantDeadTitle, KeyGrantDeadBody, KeyGrantDeadChat,
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

// TestGrantDeadCopyAvoidsRevocationBlame pins the distinction the copy exists to
// make. A stale refresh token is not a revocation: the app is still connected on
// Twitch's side, so telling the streamer their authorization was revoked sends
// them hunting in Twitch Connections for a problem that is not there.
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

// TestFallbackChain covers T's three stages: exact hit, English fallback for an
// unknown locale, and the key itself for an unknown key.
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
