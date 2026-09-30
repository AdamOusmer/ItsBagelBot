// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/// <reference types="vite/client" />
import type { Locale, MessageTree } from './types';
import { acceptedLanguages } from './accept-language';
import { assembleCatalogs, catalogLocale } from './tree';

export type { Locale } from './types';

const eagerModules = import.meta.glob<unknown>('../../../../locales/en/console/**/*.json', {
  eager: true,
  import: 'default'
});

const lazyModules = import.meta.glob<unknown>(
  ['../../../../locales/*/console/**/*.json', '!../../../../locales/en/console/**'],
  { import: 'default' }
);

type Loader = [path: string, load: () => Promise<unknown>];

const catalogs: Record<string, MessageTree> = assembleCatalogs(
  Object.entries(eagerModules).map(([path, content]) => ({ path, content }))
);

const lazyByLocale = new Map<string, Loader[]>();
for (const [path, load] of Object.entries(lazyModules)) {
  const code = catalogLocale(path);
  if (!code) continue;
  const loaders = lazyByLocale.get(code) ?? [];
  loaders.push([path, load]);
  lazyByLocale.set(code, loaders);
}

export const LOCALES: readonly Locale[] = [...new Set([...Object.keys(catalogs), ...lazyByLocale.keys()])].sort();
export const DEFAULT_LOCALE: Locale = 'en';

const pending = new Map<string, Promise<void>>();

async function loadCatalog(locale: Locale, loaders: Loader[]): Promise<void> {
  const files = await Promise.all(loaders.map(async ([path, load]) => ({ path, content: await load() })));
  catalogs[locale] = assembleCatalogs(files)[locale] ?? {};
}

export function ensureCatalog(locale: Locale): Promise<void> {
  if (Object.prototype.hasOwnProperty.call(catalogs, locale)) return Promise.resolve();
  const loaders = lazyByLocale.get(locale);
  if (!loaders) return Promise.resolve();
  let p = pending.get(locale);
  if (!p) {
    p = loadCatalog(locale, loaders)
      .catch(() => {
        // Never reject: callers await this from the root load, so a rejection would 500 every route.
      })
      .finally(() => {
        pending.delete(locale);
      });
    pending.set(locale, p);
  }
  return p;
}

if (!catalogs[DEFAULT_LOCALE]) {
  throw new Error(
    `i18n: missing catalog for DEFAULT_LOCALE '${DEFAULT_LOCALE}' ` +
      `(expected locales/${DEFAULT_LOCALE}/console/). ` +
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
  return acceptedLanguages(opts.accept).find(isLocale) ?? DEFAULT_LOCALE;
}
