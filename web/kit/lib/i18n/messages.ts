// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/// <reference types="vite/client" />
import { groupNames, loaders, localeNames, locales, urls } from 'virtual:bagel-i18n';
import type { Locale, MessageTree } from './types';
import { matchAcceptLanguage } from './accept-language';

export type { Locale } from './types';

const CORE = 'core';

export const LOCALES: readonly Locale[] = locales;
export const DEFAULT_LOCALE: Locale = 'en';

const catalogs: Record<string, MessageTree> = {};
const requestedGroups = new Set<string>();
const loadedParts = new Map<string, Promise<void>>();

async function fetchPart(locale: Locale, part: string): Promise<void> {
  const response = await fetch(urls[locale][part]);
  if (!response.ok) throw new Error(`i18n: ${response.status} for ${locale}/${part}`);
  Object.assign((catalogs[locale] ??= {}), await response.json());
}

async function importLocale(locale: Locale): Promise<void> {
  catalogs[locale] = (await loaders[locale]()).default;
}

function loadPart(locale: Locale, part: string): Promise<void> {
  const key = `${locale}/${part}`;
  let loaded = loadedParts.get(key);
  if (!loaded) {
    loaded = (import.meta.env.SSR ? importLocale(locale) : fetchPart(locale, part)).catch((error) => {
      loadedParts.delete(key);
      throw error;
    });
    loadedParts.set(key, loaded);
  }
  return loaded;
}

async function loadParts(locale: Locale): Promise<void> {
  const parts = import.meta.env.SSR ? [CORE] : [CORE, ...requestedGroups].filter((part) => urls[locale][part]);
  await Promise.all(parts.map((part) => loadPart(locale, part)));
}

export function ensureCatalog(locale: Locale, groups: readonly string[] = []): Promise<void> {
  for (const group of groups) {
    if (!groupNames.includes(group)) throw new Error(`i18n: unknown catalog group '${group}'`);
    requestedGroups.add(group);
  }
  if (!urls[locale]) return Promise.resolve();
  return loadParts(locale).catch(() => {
    // Never reject: callers await this from the root load, so a rejection would 500 every route.
    return locale === DEFAULT_LOCALE ? undefined : loadParts(DEFAULT_LOCALE).catch(() => undefined);
  });
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
