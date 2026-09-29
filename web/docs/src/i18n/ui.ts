// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import en from './locales/en.json';

const catalogs = import.meta.glob<Record<string, string>>('./locales/*.json', {
  eager: true,
  import: 'default'
});

const locales = Object.keys(catalogs)
  .map((file) => file.slice('./locales/'.length, -'.json'.length))
  .sort();

export function docsI18n(routeLocale: string | undefined) {
  const locale = routeLocale && routeLocale !== 'root' ? routeLocale : 'en';
  const catalog = catalogs[`./locales/${locale}.json`] ?? en;
  return {
    locale,
    t: (key: keyof typeof en) => catalog[key] ?? en[key],
    path: (path: string) => locale === 'en' ? path : `/${locale}${path}`
  };
}

function unlocalizedPath(pathname: string): string {
  const [, first = ''] = pathname.split('/');
  const prefixed = first !== 'en' && locales.includes(first);
  return prefixed ? pathname.slice(first.length + 1) || '/' : pathname;
}

export function docsLocaleOptions(pathname: string, current: string) {
  const bare = unlocalizedPath(pathname);
  return locales.map((code) => ({
    code,
    href: docsI18n(code).path(bare),
    label: code.toUpperCase(),
    current: code === current,
  }));
}
