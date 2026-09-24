// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Locale } from './i18n/types';

export const SITE = {
  web: 'https://itsbagelbot.com',
  dashboard: 'https://dashboard.itsbagelbot.com',
  commands: 'https://commands.itsbagelbot.com',
  docs: 'https://docs.itsbagelbot.com',
  stats: 'https://stats.itsbagelbot.com',
  status: 'https://status.itsbagelbot.com',
  github: 'https://github.com/AdamOusmer/ItsBagelBot',
  discord: 'https://discord.gg/SZ2remwSDv',
  twitch: 'https://twitch.tv/itsbagelbot',
} as const;

export interface SiteLinkDef {
  path?: string;
  url?: string;
  lang?: boolean;
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
  active: boolean;
  external: boolean;
}

export interface SiteColumn {
  title: string;
  links: SiteLink[];
}

export interface SiteLinkContext {
  path(path: string): string;
  label(key: string): string;
  langQuery: string;
  isActive?(href: string): boolean;
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
      { path: '/changelog', key: 'changelog' },
      { path: '/command-builder', key: 'builder' },
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
      { url: SITE.dashboard, key: 'dashboard', lang: true },
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
  const base = def.url ?? ctx.path(def.path ?? '/');
  const href = def.lang ? `${base}${ctx.langQuery}` : base;
  return {
    href,
    label: def.label ?? ctx.label(def.key ?? ''),
    active: ctx.isActive?.(href) ?? false,
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

export function webHref(locale: Locale, path: string): string {
  return locale === 'en' ? `${SITE.web}${path}` : `${SITE.web}/${locale}${path}`;
}

export function commandsHref(segment: string): string {
  return `${SITE.commands}/user/${segment}`;
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
): Array<{ code: Locale; href: string; label: string; current: boolean }> {
  return locales.map((code) => {
    const params = new URLSearchParams(url.search);
    params.set('lang', code);
    return {
      code,
      href: `${url.pathname}?${params}`,
      label: code.toUpperCase(),
      current: code === current,
    };
  });
}
