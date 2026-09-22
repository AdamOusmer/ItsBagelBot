import en from './locales/en.json';

const catalogs = import.meta.glob<Record<string, string>>('./locales/*.json', {
  eager: true,
  import: 'default'
});

/** Starlight uses an undefined locale for English routes at the root. */
export function docsI18n(routeLocale: string | undefined) {
  const locale = routeLocale && routeLocale !== 'root' ? routeLocale : 'en';
  const catalog = catalogs[`./locales/${locale}.json`] ?? en;
  return {
    locale,
    t: (key: keyof typeof en) => catalog[key] ?? en[key],
    path: (path: string) => locale === 'en' ? path : `/${locale}${path}`
  };
}
