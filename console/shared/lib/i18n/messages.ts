// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
//
// Only the default catalog is bundled eagerly (~53 KB of object literals that
// every boot otherwise paid for — both catalogs eager cost ~107 KB of eval
// before first paint). The other catalogs become separate chunks behind
// ensureCatalog(); routes that know their locale await it in a load function,
// so SSR and hydration always see the same strings.
import type { Locale, MessageTree } from './types';

export type { Locale } from './types';

// Eager default catalog: translate()'s fallback must exist synchronously even
// in render trees that never ran a load function (the admin app's fallback
// translator, server helpers).
const eagerModules = import.meta.glob<MessageTree>('./locales/en.json', {
  eager: true,
  import: 'default'
});

// Lazy loaders for every shipped catalog. The KEYS are known at build time with
// no content loaded, which is what keeps LOCALES/isLocale synchronous.
const lazyModules = import.meta.glob<MessageTree>('./locales/*.json', {
  import: 'default'
});

// Key each catalog by its bare filename ("en" from "./locales/en.json").
const catalogs: Record<string, MessageTree> = {};
for (const [path, tree] of Object.entries(eagerModules)) {
  const match = /([\w-]+)\.json$/.exec(path);
  if (match) catalogs[match[1]] = tree;
}

// The locale set is exactly the shipped catalogs, sorted so the switcher and any
// listing render in a stable order across builds.
export const LOCALES: readonly Locale[] = Object.keys(lazyModules)
  .map((path) => /([\w-]+)\.json$/.exec(path)?.[1])
  .filter((v): v is Locale => typeof v === 'string')
  .sort();
export const DEFAULT_LOCALE: Locale = 'en';

// Single-flight per locale: several components may ask for the same catalog in
// one tick (LangSwitch tooltips), the chunk must be fetched once.
const pending = new Map<string, Promise<void>>();

/**
 * Register a locale's catalog before any string from it is rendered. Resolves
 * immediately for the default locale (already registered) or an unknown code.
 * Await this in a universal load so hydration renders exactly what SSR did.
 */
export function ensureCatalog(locale: Locale): Promise<void> {
  if (Object.prototype.hasOwnProperty.call(catalogs, locale)) return Promise.resolve();
  const key = `./locales/${locale}.json`;
  const loader = lazyModules[key];
  if (!loader) return Promise.resolve();
  let p = pending.get(key);
  if (!p) {
    p = loader()
      .then((tree) => {
        catalogs[locale] = tree;
      })
      .finally(() => {
        pending.delete(key);
      });
    pending.set(key, p);
  }
  return p;
}

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

// The locale set comes from build-time filenames, never from request input, so
// a Set membership test has the same no-inherited-keys guarantee an own-property
// check gave ('constructor' cannot be a filename).
const LOCALE_SET: ReadonlySet<string> = new Set(LOCALES);

export function isLocale(v: unknown): v is Locale {
  return typeof v === 'string' && LOCALE_SET.has(v);
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
