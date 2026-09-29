// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package i18n

import (
	"ItsBagelBot/locales"
	"io/fs"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
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

func jsonFile(body string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(body)}
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

func TestKeyPrefix(t *testing.T) {
	cases := map[string]string{
		"loyalty.json":      "loyalty.",
		"bagels_ready.json": "bagels_ready.",
		"index.json":        "",
	}
	for file, want := range cases {
		if got := keyPrefix(file); got != want {
			t.Errorf("keyPrefix(%q) = %q, want %q", file, got, want)
		}
	}
}

func TestLoadCatalogsPrefixesKeysByFile(t *testing.T) {
	got := mustLoadCatalogs(fstest.MapFS{
		"manifest.json":         jsonFile(`["en"]`),
		"en/chat/loyalty.json":  jsonFile(`{"points.balance":"Balance","points":"Points"}`),
		"en/chat/index.json":    jsonFile(`{"ping":"Pong","uptime":"Live for {dashboard_url}"}`),
		"en/chat/uptime.json":   jsonFile(`{"offline":"Offline"}`),
		"en/console/index.json": jsonFile(`{"title":"Console"}`),
		"de/console/index.json": jsonFile(`{"title":"Konsole"}`),
	})
	want := map[string]map[string]string{"en": {
		"loyalty.points.balance": "Balance",
		"loyalty.points":         "Points",
		"ping":                   "Pong",
		"uptime":                 "Live for " + DashboardURL,
		"uptime.offline":         "Offline",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mustLoadCatalogs = %v, want %v", got, want)
	}
}

func TestLoadCatalogsPanicsOnDuplicateKey(t *testing.T) {
	requirePanic(t, "duplicate key loyalty.points", func() {
		mustLoadCatalogs(fstest.MapFS{
			"en/chat/index.json":   jsonFile(`{"loyalty.points":"Points"}`),
			"en/chat/loyalty.json": jsonFile(`{"points":"Points"}`),
		})
	})
}

func TestLoadCatalogsRequiresDefaultLocale(t *testing.T) {
	requirePanic(t, "missing required catalog en/chat", func() {
		mustLoadCatalogs(fstest.MapFS{
			"fr/chat/index.json": jsonFile(`{"ping":"Pong"}`),
			"en/console/x.json":  jsonFile(`{"title":"Console"}`),
		})
	})
}

func TestLoadCatalogsPanicsOnMalformedJSON(t *testing.T) {
	requirePanic(t, "malformed en/chat/loyalty.json", func() {
		mustLoadCatalogs(fstest.MapFS{
			"en/chat/loyalty.json": jsonFile(`{"points":`),
		})
	})
}

func isChatCatalogFile(name string) bool {
	segments := strings.Split(name, "/")
	return len(segments) >= 3 && segments[1] == chatDir && path.Ext(name) == ".json"
}

func TestEmbedCoversChatTree(t *testing.T) {
	err := fs.WalkDir(os.DirFS("../../../locales"), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isChatCatalogFile(name) {
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

func TestLoadManifestSorts(t *testing.T) {
	got := mustLoadManifest(fstest.MapFS{"manifest.json": jsonFile(`["fr","en"]`)})
	if want := []string{"en", "fr"}; !reflect.DeepEqual(got, want) {
		t.Errorf("mustLoadManifest = %v, want %v", got, want)
	}
}
