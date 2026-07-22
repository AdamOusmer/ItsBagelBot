// Marketing-site i18n. The catalogs are now pure data: one JSON file per locale
// under ./locales, discovered from the filesystem at build time. English is the
// source of truth (en.json); every other locale falls back to it per-key, so a
// missing translation renders English, never a blank. A non-technical translator
// adds a language by dropping in <code>.json</code> — no edits here. Astro's own
// i18n routing (astro.config) owns the /<locale>/ URL prefix; this module owns
// the copy and the locale-aware link/switch helpers.

import en from './locales/en.json';

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

/**
 * EN paths that have a translated twin. Single source of truth for the hreflang
 * emitter (Layout.astro) and the language switcher, which would otherwise each
 * carry their own copy of this set and drift.
 */
export const LOCALIZED_PATHS: ReadonlySet<string> = new Set([
  '/', '/pricing', '/contact', '/privacy', '/terms', '/creator-terms',
  '/guides', '/guides/getting-started', '/guides/commands', '/guides/modules',
  '/guides/counters', '/command-builder',
]);

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
 * anchors) and the default locale pass through untouched.
 */
export function localizePath(path: string, lang: Lang): string {
  if (lang === defaultLang || !path.startsWith('/')) return path;
  return path === '/' ? `/${lang}/` : `/${lang}${path}`;
}

/** The human-readable name of a locale, from its own lang.name key. */
export function languageName(lang: Lang): string {
  return catalog[lang]?.['lang.name'] ?? lang;
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
