// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Marketing-site i18n. The catalogs are now pure data: one JSON file per locale
// under ./locales, discovered from the filesystem at build time. English is the
// source of truth (en.json); every other locale falls back to it per-key, so a
// missing translation renders English, never a blank. A non-technical translator
// adds a language by dropping in <code>.json</code>, no edits here. Astro's own
// i18n routing (astro.config) owns the /<locale>/ URL prefix; this module owns
// the copy and the locale-aware link/switch helpers.

import en from './locales/en.json';
// The guide URLs come from the slug list, not a second hand-kept copy. Imported
// from lib/guides/slugs (which imports nothing) rather than from the registry,
// which imports this file: the slugs module exists to break that cycle.
import { guideLocalizedPaths } from '../lib/guides/slugs';

// Eager glob: every locale catalog, bundled at build time. Keyed by module path
// ('./locales/fr.json' → the parsed object).
const files = import.meta.glob<Record<string, string>>('./locales/*.json', {
  eager: true,
  import: 'default',
});

/** A locale code (e.g. 'en', 'fr'). Open set: whatever JSON files exist. */
export type Lang = string;
/** A translation key. English (en.json) is the canonical key set. */
export type UIKey = keyof typeof en;

export const defaultLang: Lang = 'en';

// Build the locale → catalog map from the discovered files.
const catalog: Record<string, Record<string, string>> = {};
for (const path in files) {
  const code = path.slice(path.lastIndexOf('/') + 1, -'.json'.length);
  catalog[code] = files[path];
}

/** Every locale found on disk, sorted. Drives hreflang and the switcher. */
export const locales: readonly Lang[] = Object.freeze(Object.keys(catalog).sort());

const EN_KEYS: readonly string[] = Object.keys(en);
const EN_KEY_SET: ReadonlySet<string> = new Set(EN_KEYS);
const EMPTY: Record<string, string> = {};

/** Own-property-safe locale membership (never walks the prototype chain). */
function hasLocale(code: string): boolean {
  return Object.prototype.hasOwnProperty.call(catalog, code);
}

function normalizePath(path: string): string {
  return path.replace(/\/+$/, '') || '/';
}

// Per-release changelog paths, discovered from the content files the same way
// the pages are. Keyed on the version field, matching [version].astro's params.
const changelogFiles = import.meta.glob<{ version: string }>(
  '../content/changelog/*.json',
  { eager: true, import: 'default' },
);
const changelogLocalizedPaths = Object.values(changelogFiles).map(
  (entry) => `/changelog/${entry.version}`,
);

/**
 * EN paths that have a translated twin. Single source of truth for the hreflang
 * emitter (Layout.astro) and the language switcher, which would otherwise each
 * carry their own copy of this set and drift.
 */
export const LOCALIZED_PATHS: ReadonlySet<string> = new Set([
  '/', '/pricing', '/contact', '/privacy', '/terms', '/creator-terms',
  ...guideLocalizedPaths, '/command-builder', '/changelog', '/song-requests',
  '/valorant-stats', '/import', ...changelogLocalizedPaths,
]);

/**
 * The `lang` route param for a locale: `undefined` for the default locale,
 * which lives at the root (prefixDefaultLocale:false in astro.config), the
 * code itself for every other. Exported for the two pages whose paths are a
 * locale × something product (changelog versions, guide slugs).
 */
export function langParam(lang: Lang): Lang | undefined {
  return lang === defaultLang ? undefined : lang;
}

/**
 * `getStaticPaths` for a page that exists once per locale and nothing else.
 * Twelve pages each spelled out the same map, so a new locale rule (or a fix
 * to the default-locale param) had twelve places to reach. Lives here because
 * this module already owns `locales` and `defaultLang`; a page importing it
 * cannot disagree with the router about which paths exist.
 */
export function localeStaticPaths() {
  return locales.map((l) => ({ params: { lang: langParam(l) } }));
}

/** Locale from the URL: first path segment when it names a known locale, else default. */
export function getLangFromUrl(url: URL): Lang {
  const seg = url.pathname.split('/')[1] ?? '';
  return hasLocale(seg) ? seg : defaultLang;
}

/**
 * Split a pathname into its locale and the locale-less base path. A leading
 * known non-default locale segment is stripped; the base path is normalized
 * (no trailing slash, '/' for root). '/fr/guides/' → { lang:'fr', path:'/guides' }.
 */
export function splitLocale(pathname: string): { lang: Lang; path: string } {
  const segments = pathname.split('/');
  const first = segments[1] ?? '';
  if (first !== defaultLang && hasLocale(first)) {
    return { lang: first, path: normalizePath('/' + segments.slice(2).join('/')) };
  }
  return { lang: defaultLang, path: normalizePath(pathname) };
}

/** Bound translator for a locale, English fallback per key, key as last resort. */
export function useTranslations(lang: Lang) {
  const table = catalog[lang] ?? EMPTY;
  return function t(key: UIKey): string {
    return table[key] ?? en[key] ?? key;
  };
}

/**
 * Prefix an internal path with the active locale. External URLs (http…, mailto,
 * anchors) pass through untouched. Internal paths come back with a trailing
 * slash: Cloudflare Pages 308-redirects the slashless form, so emitting it in
 * hrefs and hreflang costs a hop per click and makes hreflang disagree with the
 * canonical/sitemap URLs (Google drops mismatched hreflang pairs).
 */
export function localizePath(path: string, lang: Lang): string {
  if (!path.startsWith('/')) return path;
  const slashed = path.endsWith('/') ? path : `${path}/`;
  return lang === defaultLang ? slashed : `/${lang}${slashed}`;
}

/** The human-readable name of a locale, from its own lang.name key. */
export function languageName(lang: Lang): string {
  return catalog[lang]?.['lang.name'] ?? lang;
}

// Add to Twitch must hit the dashboard OAuth *start*, not the console origin.
// /auth/login sets the host-only state/nonce cookies, 302s to Twitch, and the
// callback lands the visitor in the console. Linking the origin instead showed
// /login first (an extra click) and never bound CSRF to the dashboard host if
// we ever built the authorize URL on this site. Footer "Dashboard" still uses
// the origin; only the CTA uses this.
const DASHBOARD_LOGIN = 'https://dashboard.itsbagelbot.com/auth/login';

/**
 * The locale switch's options for the page at `url`, in the shape
 * @bagel/ui's LanguageSwitcher takes.
 *
 * Lives here rather than in the component because TWO surfaces render the
 * switch -- the nav bar and the mobile panel -- and they were computing the
 * same fallback rule from two copies of it. A route with no translated
 * counterpart keeps the default path, so the switch never creates a dead link.
 */
export function localeOptions(url: URL) {
  const current = getLangFromUrl(url);
  const { path } = splitLocale(url.pathname);

  return locales.map((code) => ({
    code,
    href:
      code === defaultLang || !LOCALIZED_PATHS.has(path) ? path : localizePath(path, code),
    label: code.toUpperCase(),
    current: code === current,
  }));
}

export function dashLoginHref(lang: Lang): string {
  return lang === defaultLang ? DASHBOARD_LOGIN : `${DASHBOARD_LOGIN}?lang=${lang}`;
}

// Build-time parity warning: for every non-English locale, list the keys it is
// missing (rendered in English) or carries in excess (typos / stale keys). Runs
// once at module init during `astro build`; never fails the build.
for (const code of locales) {
  if (code === defaultLang) continue;
  const table = catalog[code] ?? EMPTY;
  const present = Object.keys(table);
  const presentSet = new Set(present);
  const missing = EN_KEYS.filter((k) => !presentSet.has(k));
  const extra = present.filter((k) => !EN_KEY_SET.has(k));
  if (missing.length || extra.length) {
    console.warn(
      `[i18n] locale "${code}" parity: ${missing.length} missing, ${extra.length} extra` +
        (missing.length ? `\n  missing: ${missing.join(', ')}` : '') +
        (extra.length ? `\n  extra: ${extra.join(', ')}` : ''),
    );
  }
}
