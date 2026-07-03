// Pure-TS i18n runtime: no Svelte imports, so server code (hooks, load
// functions) can import it via `@bagel/shared/i18n` without dragging component
// modules into the server graph. Component-facing context helpers live in
// context.ts.
import type { Locale, MessageTree } from './types';
import { en } from './en';
import { fr } from './fr';

export type { Locale } from './types';

export const LOCALES: readonly Locale[] = ['en', 'fr'];
export const DEFAULT_LOCALE: Locale = 'en';

// The cookie the switcher writes and hooks.server.ts reads. Not the session
// cookie: locale is a UI preference that must work pre-login (login/goodbye) and
// never needs the session key, so it rides its own plain cookie.
export const LOCALE_COOKIE = 'locale';

const catalogs: Record<Locale, MessageTree> = { en, fr };

export function isLocale(v: unknown): v is Locale {
  return typeof v === 'string' && (LOCALES as readonly string[]).includes(v);
}

/** Walk a dot-path ("settings.deleteTitle") into a catalog; string leaves only. */
function lookup(tree: MessageTree, key: string): string | undefined {
  let node: string | string[] | MessageTree | undefined = tree;
  for (const part of key.split('.')) {
    if (node == null || typeof node === 'string' || Array.isArray(node)) return undefined;
    node = node[part];
  }
  return typeof node === 'string' ? node : undefined;
}

/**
 * Resolve `key` for `locale`, filling `{name}` placeholders from `params`.
 * Falls back to English, then to the key itself, so a missing translation shows
 * English (or, worst case, a visible key) rather than a blank.
 */
export function translate(
  locale: Locale,
  key: string,
  params?: Record<string, string | number>
): string {
  let str = lookup(catalogs[locale] ?? catalogs.en, key) ?? lookup(catalogs.en, key) ?? key;
  if (params) {
    for (const name in params) {
      str = str.split(`{${name}}`).join(String(params[name]));
    }
  }
  return str;
}

/** Ordered list leaves (feature bullets). Empty array when the key is absent. */
export function translateList(locale: Locale, key: string): string[] {
  const from = (tree: MessageTree): string[] | undefined => {
    let node: string | string[] | MessageTree | undefined = tree;
    for (const part of key.split('.')) {
      if (node == null || typeof node === 'string' || Array.isArray(node)) return undefined;
      node = node[part];
    }
    return Array.isArray(node) ? node : undefined;
  };
  return from(catalogs[locale] ?? catalogs.en) ?? from(catalogs.en) ?? [];
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
