// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package i18n

import (
	"ItsBagelBot/locales"
	"fmt"
	"io/fs"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
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

func TestGaps(t *testing.T) {
	gaps := Gaps()

	if _, ok := gaps[DefaultLocale]; ok {
		t.Errorf("Gaps must exclude the default locale %q", DefaultLocale)
	}
	if got, want := sortedKeys(gaps), nonDefaultLocales(List()); !reflect.DeepEqual(got, want) {
		t.Errorf("Gaps keys = %v, want List() minus %q = %v", got, DefaultLocale, want)
	}
	for locale, missing := range gaps {
		if m := Missing(locale); !reflect.DeepEqual(missing, m) {
			t.Errorf("Gaps[%q] = %v, but Missing(%q) = %v", locale, missing, locale, m)
		}
	}
	if n := len(gaps["fr"]); n != 0 {
		t.Errorf("fr is fully translated but Gaps reports %d missing key(s): %v", n, gaps["fr"])
	}
}

func TestSupported(t *testing.T) {
	cases := map[string]bool{"en": true, "fr": true, "es": true, "pt-br": true, "de": true, "ru": true, "pt": false, "xx": false, "": false}
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

func isChatCatalogFile(name string) bool {
	segments := strings.Split(name, "/")
	return len(segments) >= 3 && segments[1] == chatDir && path.Ext(name) == ".json"
}

func skipsEmbedCheck(name string, d fs.DirEntry, err error) bool {
	return err != nil || d.IsDir() || !isChatCatalogFile(name)
}

func TestEmbedCoversChatTree(t *testing.T) {
	err := fs.WalkDir(os.DirFS("../../../locales"), ".", func(name string, d fs.DirEntry, err error) error {
		if skipsEmbedCheck(name, d, err) {
			return err
		}
		if _, statErr := fs.Stat(locales.FS, name); statErr != nil {
			t.Errorf("locales/%s is not embedded; widen the go:embed pattern in locales/embed.go", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDefaultSeriesIsTheOrderedEnglishTemplates(t *testing.T) {
	series := DefaultSeries("personality.affection")

	if len(series) < 2 {
		t.Fatalf("series = %v, want every personality.affection.N template", series)
	}
	for i, tmpl := range series {
		if key := fmt.Sprintf("personality.affection.%d", i); tmpl != T(DefaultLocale, key) {
			t.Errorf("series[%d] = %q, want the %s template", i, tmpl, key)
		}
	}
	requirePanic(t, "missing default series", func() { DefaultSeries("no.such.series") })
}

func TestWarnGapsStaysQuietWhenEveryLocaleIsComplete(t *testing.T) {
	for locale, missing := range Gaps() {
		if len(missing) > 0 {
			t.Skipf("%s has %d untranslated keys", locale, len(missing))
		}
	}
	core, logs := observer.New(zap.WarnLevel)

	WarnGaps(zap.New(core))

	if logs.Len() != 0 {
		t.Fatalf("WarnGaps logged %v for complete locales", logs.All())
	}
}

func requirePanic(t *testing.T, want string, load func()) {
	t.Helper()
	defer func() {
		got, _ := recover().(string)
		if !strings.Contains(got, want) {
			t.Errorf("panic = %q, want it to contain %q", got, want)
		}
	}()
	load()
}
