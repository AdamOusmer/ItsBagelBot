package i18n

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestGapsMechanism replaces the old strict parity test. The locked product
// decision is that a key missing from a locale WARNS and falls back to English
// rather than failing the build, so a partially translated language can ship.
// This therefore verifies the reporting mechanism instead of enforcing parity:
// Gaps is keyed by the manifest minus en, agrees with Missing for every locale
// it reports, and shows a fully translated locale (fr today) as complete.
func TestGapsMechanism(t *testing.T) {
	gaps := Gaps()

	if _, ok := gaps[DefaultLocale]; ok {
		t.Errorf("Gaps must exclude the default locale %q", DefaultLocale)
	}

	var want []string
	for _, l := range List() {
		if l != DefaultLocale {
			want = append(want, l)
		}
	}
	got := make([]string, 0, len(gaps))
	for l := range gaps {
		got = append(got, l)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Gaps keys = %v, want List() minus en = %v", got, want)
	}

	// Agreement check: Gaps for a locale is exactly Missing for that locale. This
	// stands in for a fabricated-gap case, which embed makes awkward to inject.
	for locale, missing := range gaps {
		if m := Missing(locale); !reflect.DeepEqual(missing, m) {
			t.Errorf("Gaps[%q] = %v, but Missing(%q) = %v", locale, missing, locale, m)
		}
	}

	if n := len(gaps["fr"]); n != 0 {
		t.Errorf("fr is fully translated but Gaps reports %d missing key(s): %v", n, gaps["fr"])
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

// TestDashboardTokenExpanded proves the parse-time expansion ran: no loaded
// value still carries the {dashboard_url} placeholder, and every key that
// concatenated the URL before the refactor now carries the real DashboardURL.
func TestDashboardTokenExpanded(t *testing.T) {
	for locale := range catalog {
		for key, val := range catalog[locale] {
			if strings.Contains(val, dashboardToken) {
				t.Errorf("catalog[%q][%q] still carries the raw token %q", locale, key, dashboardToken)
			}
		}
	}

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
