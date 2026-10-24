// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/// <reference types="vite/client" />
import { loaders, localeNames, locales, sliced, urls } from 'virtual:bagel-i18n';
import type { Locale, MessageTree } from './types';
import { matchAcceptLanguage } from './accept-language';

export type { Locale } from './types';

export const LOCALES: readonly Locale[] = locales;
export const DEFAULT_LOCALE: Locale = 'en';

const catalogs: Record<string, MessageTree> = {};
const loading = new Map<string, Promise<void>>();

async function fetchTree(url: string): Promise<MessageTree> {
  const response = await fetch(url);
  if (!response.ok) throw new Error(`i18n: ${response.status} for ${url}`);
  return response.json();
}

async function readCatalog(locale: Locale): Promise<void> {
  catalogs[locale] = import.meta.env.SSR ? (await loaders[locale]()).default : await fetchTree(urls[locale]);
}

function once(id: string, read: () => Promise<void>): Promise<void> {
  let loaded = loading.get(id);
  if (!loaded) {
    loaded = read().catch((error) => {
      loading.delete(id);
      throw error;
    });
    loading.set(id, loaded);
  }
  return loaded;
}

const loadCatalog = (locale: Locale): Promise<void> => once(locale, () => readCatalog(locale));

const isBranch = (node: unknown): node is MessageTree => typeof node === 'object' && node !== null && !Array.isArray(node);

function mergeInto(target: MessageTree, source: MessageTree): void {
  for (const [key, value] of Object.entries(source)) {
    if (key === '__proto__' || key === 'constructor') continue;
    const existing = target[key];
    if (isBranch(value) && isBranch(existing)) mergeInto(existing, value);
    else target[key] = value;
  }
}

const sliceBases = new Map<string, string>();
let wanted: Set<Locale> | undefined;

function documentLocale(): Locale {
  const lang = typeof document === 'undefined' ? '' : document.documentElement.lang;
  return LOCALES.includes(lang) ? lang : DEFAULT_LOCALE;
}

function loadSlice(locale: Locale, file: string): Promise<void> {
  return once(`${locale}/${file}`, async () => mergeInto((catalogs[locale] ??= {}), await fetchTree(`${sliceBases.get(file)}${locale}/${file}`)));
}

/** Called by every node or lazy chunk of a sliced build before it evaluates; an empty `h` means no slice to fetch. */
export async function registerMessages(chunkUrl: string, messages: { h: string; m: MessageTree }): Promise<void> {
  mergeInto((catalogs[DEFAULT_LOCALE] ??= {}), messages.m);
  if (!messages.h) return;
  const file = chunkUrl.slice(chunkUrl.lastIndexOf('/') + 1).replace(/\.js$/, '.json');
  sliceBases.set(file, new URL('../i18n/', chunkUrl).href);
  wanted ??= new Set([documentLocale()]);
  const others = [...wanted].filter((locale) => locale !== DEFAULT_LOCALE);
  await Promise.allSettled(others.map((locale) => loadSlice(locale, file)));
}

async function ensureSlices(locale: Locale): Promise<boolean> {
  (wanted ??= new Set()).add(locale);
  if (locale === DEFAULT_LOCALE) return true;
  const results = await Promise.allSettled([...sliceBases.keys()].map((file) => loadSlice(locale, file)));
  return results.every((result) => result.status === 'fulfilled');
}

export async function ensureCatalog(locale: Locale): Promise<boolean> {
  if (sliced && !import.meta.env.SSR) return ensureSlices(locale);
  if (!(import.meta.env.SSR ? loaders[locale] : urls[locale])) return true;
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
