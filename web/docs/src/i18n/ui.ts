// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { flatCatalogs, type FlatTree } from '@bagel/kit/i18n/flat';

const catalogs = flatCatalogs(
  import.meta.glob<FlatTree>('../../../../locales/*/docs/**/*.json', { eager: true, import: 'default' }),
  'docs',
);
const en = catalogs.en;

const locales = Object.keys(catalogs).sort();

export function docsI18n(routeLocale: string | undefined) {
  const locale = routeLocale && routeLocale !== 'root' ? routeLocale : 'en';
  const catalog = catalogs[locale] ?? en;
  return {
    locale,
    t: (key: string) => catalog[key] ?? en[key],
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
