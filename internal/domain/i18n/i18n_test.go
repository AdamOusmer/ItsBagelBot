package i18n

import (
	"strings"
	"testing"
)

// TestLocaleParity fails on a half-translated addition. Without it, a key added
// to en but forgotten in fr degrades silently through T's English fallback, so a
// French streamer gets an English line in their own chat and nothing reports it.
func TestLocaleParity(t *testing.T) {
	for _, locale := range Locales() {
		if locale == DefaultLocale {
			continue
		}
		if missing := Missing(locale); len(missing) > 0 {
			t.Errorf("locale %q is missing %d key(s) present in %q: %v",
				locale, len(missing), DefaultLocale, missing)
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
