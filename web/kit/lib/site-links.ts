// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Locale } from './i18n/types';

export const SITE = {
  web: 'https://itsbagelbot.com',
  dashboard: 'https://dashboard.itsbagelbot.com',
  commands: 'https://commands.itsbagelbot.com',
  leaderboard: 'https://leaderboard.itsbagelbot.com',
  docs: 'https://docs.itsbagelbot.com',
  stats: 'https://stats.itsbagelbot.com',
  status: 'https://status.itsbagelbot.com',
  github: 'https://github.com/AdamOusmer/ItsBagelBot',
  newIssue: 'https://github.com/AdamOusmer/ItsBagelBot/issues/new',
  supportEmail: 'support@itsbagelbot.com',
  enterpriseEmail: 'enterprise@itsbagelbot.com',
  discord: 'https://discord.gg/SZ2remwSDv',
  twitch: 'https://twitch.tv/itsbagelbot',
} as const;

export interface SiteLinkDef {
  path?: string;
  url?: string;
  lang?: boolean;
  locales?: readonly Locale[];
  key?: string;
  label?: string;
}

export interface SiteColumnDef {
  key: string;
  links: readonly SiteLinkDef[];
}

export interface SiteLink {
  href: string;
  label: string;
  current: boolean;
  external: boolean;
}

export interface SiteColumn {
  title: string;
  links: SiteLink[];
}

export interface SiteLinkContext {
  path(path: string, locales?: readonly Locale[]): string;
  label(key: string): string;
  langQuery: string;
  isActive?(href: string): boolean;
}

export const COMPARE_LOCALES: readonly Locale[] = ['de', 'en', 'es', 'fr', 'pt-br', 'ru'];

export function linkLocale(locale: Locale, only?: readonly Locale[]): Locale {
  return only && !only.includes(locale) ? 'en' : locale;
}

export const SITE_NAV = [
  { path: '/pricing', key: 'pricing' },
  { path: '/guides', key: 'guides' },
  { path: '/contact', key: 'contact' },
  { url: SITE.stats, key: 'stats' },
  { url: SITE.status, key: 'status' },
] as const satisfies readonly SiteLinkDef[];

export const SITE_FOOTER = [
  {
    key: 'product',
    links: [
      { path: '/pricing', key: 'pricing' },
      { path: '/guides', key: 'guides' },
      { path: '/command-builder', key: 'builder' },
      { path: '/changelog', key: 'changelog' },
    ],
  },
  {
    key: 'company',
    links: [
      { path: '/contact', key: 'contact' },
      { url: SITE.stats, key: 'stats' },
      { url: SITE.status, key: 'status' },
    ],
  },
  {
    key: 'community',
    links: [
      { url: SITE.discord, label: 'Discord' },
      { url: SITE.twitch, label: 'Twitch' },
      { url: SITE.github, label: 'GitHub' },
      { url: SITE.docs, key: 'developer' },
    ],
  },
];

export const SITE_LEGAL: readonly SiteLinkDef[] = [
  { path: '/privacy', key: 'privacy' },
  { path: '/terms', key: 'terms' },
  { path: '/creator-terms', key: 'creatorTerms' },
];

export function resolveSiteLink(def: SiteLinkDef, ctx: SiteLinkContext): SiteLink {
  const external = def.url !== undefined;
  const base = def.url ?? ctx.path(def.path ?? '/', def.locales);
  const href = def.lang ? `${base}${ctx.langQuery}` : base;
  return {
    href,
    label: def.label ?? ctx.label(def.key ?? ''),
    current: ctx.isActive?.(href) ?? false,
    external,
  };
}

export function resolveSiteLinks(
  defs: readonly SiteLinkDef[],
  ctx: SiteLinkContext,
): SiteLink[] {
  return defs.map((def) => resolveSiteLink(def, ctx));
}

export function resolveSiteColumns(
  defs: readonly SiteColumnDef[],
  ctx: SiteLinkContext,
): SiteColumn[] {
  return defs.map((column) => ({
    title: ctx.label(column.key),
    links: resolveSiteLinks(column.links, ctx),
  }));
}

export interface SiteNavInput {
  home: string;
  logoSrc: string;
  links: SiteLink[];
  ctaHref: string;
  text: {
    cta: string;
    aria: string;
    menuOpen: string;
    menuClose: string;
    menu: string;
    menuMeta: string;
  };
}

export function siteNavProps({ home, logoSrc, links, ctaHref, text }: SiteNavInput) {
  return {
    brand: { title: 'ItsBagelBot', href: home, logoSrc, logoAlt: '' },
    links,
    cta: { href: ctaHref, label: text.cta, icon: 'twitch' as const },
    label: text.aria,
    menuLabels: { open: text.menuOpen, close: text.menuClose, panel: text.menu },
    menuMeta: text.menuMeta,
  };
}

export function webHref(locale: Locale, path: string): string {
  return locale === 'en' ? `${SITE.web}${path}` : `${SITE.web}/${locale}${path}`;
}

export function commandsHref(segment: string): string {
  return `${SITE.commands}/user/${segment}`;
}

export function leaderboardHref(segment: string): string {
  return `${SITE.leaderboard}/${segment}`;
}

export function dashboardHref(path: string, langQuery: string): string {
  if (!langQuery) return `${SITE.dashboard}${path}`;
  const separator = path.includes('?') ? '&' : '?';
  return `${SITE.dashboard}${path}${separator}${langQuery.slice(1)}`;
}

export function localeOptions(
  locales: readonly Locale[],
  current: Locale,
  url: URL,
  nameOf: (code: Locale) => string,
): Array<{ code: Locale; href: string; label: string; title: string; flag?: string; current: boolean }> {
  return locales.map((code) => {
    const params = new URLSearchParams(url.search);
    params.set('lang', code);
    return {
      code,
      href: `${url.pathname}?${params}`,
      label: code.toUpperCase(),
      title: nameOf(code),
      flag: localeFlag(code),
      current: code === current,
    };
  });
}

const FLAG_OVERRIDES: Readonly<Record<string, string>> = { en: 'gb' };

const flagByLocale = new Map<string, string | undefined>();

/** Lowercase region code of the flag shown beside a locale: `pt-br` -> `br`, `es` -> `es`. */
export function localeFlag(code: Locale): string | undefined {
  if (flagByLocale.has(code)) return flagByLocale.get(code);
  const flag = Object.hasOwn(FLAG_OVERRIDES, code)
    ? FLAG_OVERRIDES[code]
    : new Intl.Locale(code).maximize().region?.toLowerCase();
  flagByLocale.set(code, flag);
  return flag;
}
