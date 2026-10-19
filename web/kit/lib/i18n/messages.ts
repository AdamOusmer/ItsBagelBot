// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/// <reference types="vite/client" />
import { loaders, localeNames, locales, urls } from 'virtual:bagel-i18n';
import type { Locale, MessageTree } from './types';
import { matchAcceptLanguage } from './accept-language';

export type { Locale } from './types';

export const LOCALES: readonly Locale[] = locales;
export const DEFAULT_LOCALE: Locale = 'en';

const catalogs: Record<string, MessageTree> = {};
const loading = new Map<string, Promise<void>>();

async function readCatalog(locale: Locale): Promise<void> {
  if (import.meta.env.SSR) {
    catalogs[locale] = (await loaders[locale]()).default;
    return;
  }
  const response = await fetch(urls[locale]);
  if (!response.ok) throw new Error(`i18n: ${response.status} for ${locale}`);
  catalogs[locale] = await response.json();
}

function loadCatalog(locale: Locale): Promise<void> {
  let loaded = loading.get(locale);
  if (!loaded) {
    loaded = readCatalog(locale).catch((error) => {
      loading.delete(locale);
      throw error;
    });
    loading.set(locale, loaded);
  }
  return loaded;
}

export async function ensureCatalog(locale: Locale): Promise<boolean> {
  if (!urls[locale]) return true;
  try {
    await loadCatalog(locale);
    return true;
  } catch {
    if (locale !== DEFAULT_LOCALE) await loadCatalog(DEFAULT_LOCALE).catch(() => undefined);
    return false;
  }
}

if (import.meta.env.SSR) await ensureCatalog(DEFAULT_LOCALE);

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
  return localeNames[code] ?? code;
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
  return matchAcceptLanguage(opts.accept, LOCALES) ?? DEFAULT_LOCALE;
}
