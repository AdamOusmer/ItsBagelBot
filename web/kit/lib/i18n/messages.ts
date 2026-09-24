// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/// <reference types="vite/client" />
import type { Locale, MessageTree } from './types';

export type { Locale } from './types';

const eagerModules = import.meta.glob<MessageTree>('./locales/en.json', {
  eager: true,
  import: 'default'
});

const lazyModules = import.meta.glob<MessageTree>('./locales/*.json', {
  import: 'default'
});

const catalogs: Record<string, MessageTree> = {};
for (const [path, tree] of Object.entries(eagerModules)) {
  const match = /([\w-]+)\.json$/.exec(path);
  if (match) catalogs[match[1]] = tree;
}

export const LOCALES: readonly Locale[] = Object.keys(lazyModules)
  .map((path) => /([\w-]+)\.json$/.exec(path)?.[1])
  .filter((v): v is Locale => typeof v === 'string')
  .sort();
export const DEFAULT_LOCALE: Locale = 'en';

const pending = new Map<string, Promise<void>>();

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
      .catch(() => {
        // Never reject: callers await this from the root load, so a rejection would 500 every route.
      })
      .finally(() => {
        pending.delete(key);
      });
    pending.set(key, p);
  }
  return p;
}

if (!catalogs[DEFAULT_LOCALE]) {
  throw new Error(
    `i18n: missing catalog for DEFAULT_LOCALE '${DEFAULT_LOCALE}' ` +
      `(expected shared/lib/i18n/locales/${DEFAULT_LOCALE}.json). ` +
      `Found: ${LOCALES.join(', ') || '(none)'}`
  );
}

export const LOCALE_COOKIE = 'locale';

const LOCALE_SET: ReadonlySet<string> = new Set(LOCALES);

export function isLocale(v: unknown): v is Locale {
  return typeof v === 'string' && LOCALE_SET.has(v);
}

function lookup(tree: MessageTree | undefined, key: string): string | undefined {
  let node: string | string[] | MessageTree | undefined = tree;
  for (const part of key.split('.')) {
    if (node == null || typeof node === 'string' || Array.isArray(node)) return undefined;
    node = node[part];
  }
  return typeof node === 'string' ? node : undefined;
}

export function localeName(code: string): string {
  return lookup(catalogs[code], 'lang.name') ?? code;
}

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

export function detectLocale(opts: { cookie?: string | null; accept?: string | null }): Locale {
  if (isLocale(opts.cookie)) return opts.cookie;
  for (const part of (opts.accept ?? '').split(',')) {
    const tag = part.trim().split(';')[0].trim().toLowerCase();
    const base = tag.split('-')[0];
    if (isLocale(base)) return base;
  }
  return DEFAULT_LOCALE;
}
