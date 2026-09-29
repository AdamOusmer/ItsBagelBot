// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { CATALOG_FILES } from '../locales/index';

type Leaves<T, P extends string = ''> = {
  [K in keyof T & string]: T[K] extends string ? `${P}${K}` : Leaves<T[K], `${P}${K}.`>;
}[keyof T & string];

export type UiMessageKey = Leaves<(typeof CATALOG_FILES)['en']>;
export type UiLocale = keyof typeof CATALOG_FILES;
export type UiParams = Record<string, string | number>;
export type UiOverride = (key: UiMessageKey, locale: UiLocale) => string | undefined;

export interface UiI18n {
  readonly locale: UiLocale;
  t(key: UiMessageKey, params?: UiParams): string;
}

export const UI_LOCALES = Object.keys(CATALOG_FILES).sort() as UiLocale[];
export const UI_DEFAULT_LOCALE: UiLocale = 'en';

function flatten(tree: object, prefix = '', out = new Map<string, string>()): Map<string, string> {
  for (const [key, value] of Object.entries(tree)) {
    if (typeof value === 'string') out.set(prefix + key, value);
    else flatten(value as object, `${prefix}${key}.`, out);
  }
  return out;
}

const CATALOGS = Object.fromEntries(
  UI_LOCALES.map((code) => [code, flatten(CATALOG_FILES[code])]),
) as Record<UiLocale, Map<string, string>>;

const isUiLocale = (code: string): code is UiLocale => Object.hasOwn(CATALOG_FILES, code);

export function resolveUiLocale(input: string | null | undefined): UiLocale {
  const code = (input ?? '').toLowerCase().replace('_', '-');
  if (isUiLocale(code)) return code;
  const base = code.split('-')[0];
  return isUiLocale(base) ? base : UI_DEFAULT_LOCALE;
}

function format(template: string, params?: UiParams): string {
  if (!params) return template;
  return template.replace(/\{([A-Za-z_][A-Za-z_0-9]*)\}/g, (match, name: string) =>
    name in params ? String(params[name]) : match,
  );
}

export function uiText(locale: UiLocale, key: UiMessageKey, params?: UiParams): string {
  return format(CATALOGS[locale].get(key) ?? CATALOGS.en.get(key) ?? key, params);
}

export function createUiI18n(locale: () => string | null | undefined, override?: UiOverride): UiI18n {
  return {
    get locale() {
      return resolveUiLocale(locale());
    },
    t(key, params) {
      const resolved = resolveUiLocale(locale());
      const custom = override?.(key, resolved);
      return custom === undefined ? uiText(resolved, key, params) : format(custom, params);
    },
  };
}
