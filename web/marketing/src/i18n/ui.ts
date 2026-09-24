// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dashboardHref } from '@bagel/kit/site-links';
import en from './locales/en.json';
import { guideLocalizedPaths } from '../lib/guides/slugs';
import { defaultLang, type Lang } from './lang';
export { defaultLang, type Lang };

const files = import.meta.glob<Record<string, string>>('./locales/*.json', {
  eager: true,
  import: 'default',
});

export type UIKey = keyof typeof en;

const catalog: Record<string, Record<string, string>> = {};
for (const path in files) {
  const code = path.slice(path.lastIndexOf('/') + 1, -'.json'.length);
  catalog[code] = files[path];
}

export const locales: readonly Lang[] = Object.freeze(Object.keys(catalog).sort());

const EN_KEYS: readonly string[] = Object.keys(en);
const EN_KEY_SET: ReadonlySet<string> = new Set(EN_KEYS);
const EMPTY: Record<string, string> = {};

function hasLocale(code: string): boolean {
  return Object.prototype.hasOwnProperty.call(catalog, code);
}

function normalizePath(path: string): string {
  return path.replace(/\/+$/, '') || '/';
}

const changelogFiles = import.meta.glob<{ version: string }>(
  '../content/changelog/*.json',
  { eager: true, import: 'default' },
);
const changelogLocalizedPaths = Object.values(changelogFiles).map(
  (entry) => `/changelog/${entry.version}`,
);

export const LOCALIZED_PATHS: ReadonlySet<string> = new Set([
  '/', '/pricing', '/contact', '/privacy', '/terms', '/creator-terms',
  ...guideLocalizedPaths, '/guides/variables', '/command-builder', '/changelog', '/song-requests',
  '/valorant-stats', '/import', ...changelogLocalizedPaths,
]);

export function langParam(lang: Lang): Lang | undefined {
  return lang === defaultLang ? undefined : lang;
}

export function localeStaticPaths() {
  return locales.map((l) => ({ params: { lang: langParam(l) } }));
}

export function getLangFromUrl(url: URL): Lang {
  const seg = url.pathname.split('/')[1] ?? '';
  return hasLocale(seg) ? seg : defaultLang;
}

export function splitLocale(pathname: string): { lang: Lang; path: string } {
  const segments = pathname.split('/');
  const first = segments[1] ?? '';
  if (first !== defaultLang && hasLocale(first)) {
    return { lang: first, path: normalizePath('/' + segments.slice(2).join('/')) };
  }
  return { lang: defaultLang, path: normalizePath(pathname) };
}

export function useTranslations(lang: Lang) {
  const table = catalog[lang] ?? EMPTY;
  return function t(key: UIKey): string {
    return table[key] ?? en[key] ?? key;
  };
}

export function localizePath(path: string, lang: Lang): string {
  if (!path.startsWith('/')) return path;
  const slashed = path.endsWith('/') ? path : `${path}/`;
  return lang === defaultLang ? slashed : `/${lang}${slashed}`;
}

export function languageName(lang: Lang): string {
  return catalog[lang]?.['lang.name'] ?? lang;
}

export function localeOptions(url: URL) {
  const current = getLangFromUrl(url);
  const { path } = splitLocale(url.pathname);

  return locales.map((code) => ({
    code,
    href:
      code === defaultLang || !LOCALIZED_PATHS.has(path) ? path : localizePath(path, code),
    label: code.toUpperCase(),
    current: code === current,
  }));
}

export function dashLoginHref(lang: Lang): string {
  return dashboardHref('/auth/login', lang === defaultLang ? '' : `?lang=${lang}`);
}

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
