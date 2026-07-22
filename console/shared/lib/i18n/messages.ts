/// <reference types="vite/client" />
// Pure-TS i18n runtime: no Svelte imports, so server code (hooks, load
// functions) can import it via `@bagel/shared/i18n` without dragging component
// modules into the server graph. Component-facing context helpers live in
// context.ts.
//
// Catalogs are plain JSON data discovered at build time by Vite's
// import.meta.glob: every shared/lib/i18n/locales/*.json becomes a locale keyed
// by its filename. A translator ships a new language by dropping one JSON file
// in that directory — no code edit, no registry entry — and their file is parsed
// as data, never executed. Malformed JSON fails the build; a missing key falls
// back to English (see translate()).
import type { Locale, MessageTree } from './types';

export type { Locale } from './types';

// Eagerly load every locale JSON as its default export (the parsed tree). The
// glob is resolved at build time, so the shipped locale set is fixed in the
// bundle rather than read from disk at runtime.
const modules = import.meta.glob<MessageTree>('./locales/*.json', {
  eager: true,
  import: 'default'
});

// Key each catalog by its bare filename ("en" from "./locales/en.json").
const catalogs: Record<string, MessageTree> = {};
for (const [path, tree] of Object.entries(modules)) {
  const match = /([\w-]+)\.json$/.exec(path);
  if (match) catalogs[match[1]] = tree;
}

// The locale set is exactly the shipped catalogs, sorted so the switcher and any
// listing render in a stable order across builds.
export const LOCALES: readonly Locale[] = Object.keys(catalogs).sort();
export const DEFAULT_LOCALE: Locale = 'en';

// The default catalog is the fallback for every missing key and the last resort
// of detectLocale, so its absence is a build/deploy error, not a silent
// English-less runtime.
if (!catalogs[DEFAULT_LOCALE]) {
  throw new Error(
    `i18n: missing catalog for DEFAULT_LOCALE '${DEFAULT_LOCALE}' ` +
      `(expected shared/lib/i18n/locales/${DEFAULT_LOCALE}.json). ` +
      `Found: ${LOCALES.join(', ') || '(none)'}`
  );
}

// The cookie the switcher writes and hooks.server.ts reads. Not the session
// cookie: locale is a UI preference that must work pre-login (login/goodbye) and
// never needs the session key, so it rides its own plain cookie.
export const LOCALE_COOKIE = 'locale';

// Own-property check only: `v in catalogs` would accept inherited names like
// 'constructor' or 'toString' coming from an attacker-controlled cookie or query
// value, so test the catalog map's own keys with hasOwnProperty.
export function isLocale(v: unknown): v is Locale {
  return typeof v === 'string' && Object.prototype.hasOwnProperty.call(catalogs, v);
}

/** Walk a dot-path ("settings.deleteTitle") into a catalog; string leaves only. */
function lookup(tree: MessageTree | undefined, key: string): string | undefined {
  let node: string | string[] | MessageTree | undefined = tree;
  for (const part of key.split('.')) {
    if (node == null || typeof node === 'string' || Array.isArray(node)) return undefined;
    node = node[part];
  }
  return typeof node === 'string' ? node : undefined;
}

/** Native self-label for a locale (its own `lang.name` leaf), or the code itself. */
export function localeName(code: string): string {
  return lookup(catalogs[code], 'lang.name') ?? code;
}

/**
 * Resolve `key` for `locale`, filling `{name}` placeholders from `params`.
 * Falls back to the default locale, then to the key itself, so a missing
 * translation shows English (or, worst case, a visible key) rather than a blank.
 */
export function translate(
  locale: Locale,
  key: string,
  params?: Record<string, string | number>
): string {
  let str =
    lookup(catalogs[locale] ?? catalogs[DEFAULT_LOCALE], key) ??
    lookup(catalogs[DEFAULT_LOCALE], key) ??
    key;
  if (params) {
    for (const name in params) {
      str = str.split(`{${name}}`).join(String(params[name]));
    }
  }
  return str;
}

/** Ordered list leaves (feature bullets). Empty array when the key is absent. */
export function translateList(locale: Locale, key: string): string[] {
  const from = (tree: MessageTree | undefined): string[] | undefined => {
    let node: string | string[] | MessageTree | undefined = tree;
    for (const part of key.split('.')) {
      if (node == null || typeof node === 'string' || Array.isArray(node)) return undefined;
      node = node[part];
    }
    return Array.isArray(node) ? node : undefined;
  };
  return from(catalogs[locale] ?? catalogs[DEFAULT_LOCALE]) ?? from(catalogs[DEFAULT_LOCALE]) ?? [];
}

/**
 * Server-side locale resolution. An explicit cookie (set by the switcher) always
 * wins; otherwise fall back to the browser's Accept-Language, then the default.
 */
export function detectLocale(opts: { cookie?: string | null; accept?: string | null }): Locale {
  if (isLocale(opts.cookie)) return opts.cookie;
  for (const part of (opts.accept ?? '').split(',')) {
    const tag = part.trim().split(';')[0].trim().toLowerCase();
    const base = tag.split('-')[0];
    if (isLocale(base)) return base;
  }
  return DEFAULT_LOCALE;
}
