// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { flatCatalogs, type FlatTree } from '@bagel/kit/i18n/flat';
import { localeFlag, webHref } from '@bagel/kit/site-links';

const catalogs = flatCatalogs(
  import.meta.glob<FlatTree>('../../../../locales/*/docs/**/*.json', { eager: true, import: 'default' }),
  'docs',
);
const en = catalogs.en;

const footers = import.meta.glob<Record<string, string>>('../../../../locales/*/website/footer.json', { eager: true, import: 'default' });
const footerCopy = (locale: string) => footers[`../../../../locales/${locale}/website/footer.json`] ?? {};

export const docsLocales = Object.keys(catalogs).sort();

export function docsI18n(routeLocale: string | undefined) {
  const locale = routeLocale ?? 'en';
  const catalog = catalogs[locale] ?? en;
  return {
    locale,
    t: (key: string) => catalog[key] ?? en[key],
    path: (path: string) => locale === 'en' ? path : `/${locale}${path}`,
  };
}

export function footerLabels(locale: string) {
  const english = footerCopy('en');
  const localized = footerCopy(locale);
  return {
    label: (key: string) => localized[key] ?? english[key],
    path: (path: string) => webHref(locale, path),
  };
}

function unlocalizedPath(pathname: string): string {
  const [, first = ''] = pathname.split('/');
  return first !== 'en' && docsLocales.includes(first) ? pathname.slice(first.length + 1) || '/' : pathname;
}

export function docsLocaleOptions(pathname: string, current: string) {
  const bare = unlocalizedPath(pathname);
  return docsLocales.map((code) => {
    const { path, t } = docsI18n(code);
    return {
      code,
      href: path(bare),
      label: code.toUpperCase(),
      title: t('lang.name'),
      flag: localeFlag(code),
      current: code === current,
    };
  });
}
