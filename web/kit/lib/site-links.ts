// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The public chrome's destinations: the nav bar's link row, the three footer
 * columns and the legal strip, as data.
 *
 * ONE definition, because there are two renderers: the marketing site resolves
 * it in web/marketing/src/layouts/Layout.astro and hands it to
 * @bagel/ui/astro/{Nav,Footer}.astro, and the console's signed-out layout
 * resolves it in web/dashboard/src/routes/(public)/+layout.svelte and hands it
 * to @bagel/ui/svelte/{Nav,Footer}.svelte. Both render the SAME components, so
 * the markup was never the thing that drifted — the LISTS were, and for as long
 * as each site held its own literal they drifted in every direction at once:
 * the console's bar was missing Status, its sign-in page declared a fourth,
 * shorter row of its own, its footer had GitHub filed under Company instead of
 * Community and had never picked up Changelog at all. Same site, four
 * navigations.
 *
 * This lives in kit rather than in ui because every line of it is the bot's:
 * its hosts, its routes, its catalog keys. The design library takes resolved
 * links as props and is not allowed to know any of them.
 *
 * Deliberately framework-free, and NOT re-exported from lib/index.ts: an Astro
 * component imports this module directly, and the barrel pulls Svelte
 * components behind it, which an Astro build cannot follow.
 */

import type { Locale } from './i18n/types';

/** Every origin the chrome points at. One place, so a host move is one edit. */
export const SITE = {
  /** The marketing site. Paths under it localize as /<locale>/<path>. */
  web: 'https://itsbagelbot.com',
  /** The console. No /<locale> routes — it reads ?lang=, hence `lang` below. */
  dashboard: 'https://dashboard.itsbagelbot.com',
  /** The short host the bot hands out for a channel's public command page. */
  commands: 'https://commands.itsbagelbot.com',
  docs: 'https://docs.itsbagelbot.com',
  stats: 'https://stats.itsbagelbot.com',
  status: 'https://status.itsbagelbot.com',
  github: 'https://github.com/AdamOusmer/ItsBagelBot',
  discord: 'https://discord.gg/SZ2remwSDv',
  twitch: 'https://twitch.tv/itsbagelbot',
} as const;

/**
 * One chrome link before a site resolves it. Exactly one of `path` / `url` is
 * set, and exactly one of `key` / `label`.
 */
export interface SiteLinkDef {
  /** A marketing-site path, handed to the resolver's `path()` to localize. */
  path?: string;
  /** An off-site URL. Never localized; renders as an external link. */
  url?: string;
  /**
   * Append the visitor's language to `url` as a query. Only the console wants
   * this: it has no localized routes, so a link into it carries `?lang=`.
   */
  lang?: boolean;
  /** Catalog key relative to the group; the resolver prefixes its own scope. */
  key?: string;
  /** A proper noun, which no catalog translates. */
  label?: string;
}

/** A footer column before resolution. `key` names the column heading. */
export interface SiteColumnDef {
  key: string;
  links: readonly SiteLinkDef[];
}

/**
 * A resolved link. Structurally the design library's `UiNavLink` — declared
 * here rather than imported so that kit does not depend on ui's type module
 * just to describe its own data.
 */
export interface SiteLink {
  href: string;
  label: string;
  active: boolean;
  external: boolean;
}

/** A resolved footer column, as `.bb-footer` wants it. */
export interface SiteColumn {
  title: string;
  links: SiteLink[];
}

/** What a site supplies to turn the definitions above into real links. */
export interface SiteLinkContext {
  /**
   * A marketing-site URL for a path, in the visitor's language.
   *
   * Marketing returns a RELATIVE localized path (`/fr/pricing/`) so its own
   * links stay on-site and keep Astro's prefetching; the dashboard returns an
   * absolute `https://itsbagelbot.com/...`, because from the console every one
   * of these leaves the app. The resolver does not care which it gets, which
   * is the reason it is a callback and not a string template.
   */
  path(path: string): string;
  /** The label for a `key`, already scoped to this site's catalog. */
  label(key: string): string;
  /** `?lang=fr`, or '' on the default locale. Used by `lang` defs only. */
  langQuery: string;
  /** True when `href` is the page being rendered. Omit to light nothing. */
  isActive?(href: string): boolean;
}

/** The nav bar's row, in order. */
export const SITE_NAV = [
  { path: '/pricing', key: 'pricing' },
  { path: '/guides', key: 'guides' },
  { path: '/contact', key: 'contact' },
  { url: SITE.stats, key: 'stats' },
  { url: SITE.status, key: 'status' },
] as const satisfies readonly SiteLinkDef[];

/** The footer's columns, in order. */
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

/** The bottom strip. */
export const SITE_LEGAL: readonly SiteLinkDef[] = [
  { path: '/privacy', key: 'privacy' },
  { path: '/terms', key: 'terms' },
  { path: '/creator-terms', key: 'creatorTerms' },
];

/** Resolve one definition against a site's routing and catalog. */
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

/** Resolve a row. */
export function resolveSiteLinks(
  defs: readonly SiteLinkDef[],
  ctx: SiteLinkContext,
): SiteLink[] {
  return defs.map((def) => resolveSiteLink(def, ctx));
}

/**
 * Resolve the footer's columns. The heading and the links come out of the same
 * catalog scope, so a column's title is `label(column.key)`.
 */
export function resolveSiteColumns(
  defs: readonly SiteColumnDef[],
  ctx: SiteLinkContext,
): SiteColumn[] {
  return defs.map((column) => ({
    title: ctx.label(column.key),
    links: resolveSiteLinks(column.links, ctx),
  }));
}

/**
 * A channel's public command page, always absolute.
 *
 * The app answers /user/<login> on every hostname it serves, so a RELATIVE link
 * keeps the visitor on whichever host they were already on. That is how
 * leaderboard.itsbagelbot.com/user/<login> came to exist and serve the commands
 * page under the leaderboard origin: the board page linked here relatively. The
 * server 308s that back to the canonical host now, so this helper is about not
 * making every visitor pay for the redirect.
 *
 * Takes the raw URL segment, not a login, because the stats boards link some
 * channels by id when their display name cannot be one.
 */
export function commandsHref(segment: string): string {
  return `${SITE.commands}/user/${segment}`;
}

/**
 * A URL into the console, carrying the visitor's language.
 *
 * The console has no localized routes, so the language rides as `?lang=`;
 * `langQuery` is the `'?lang=fr'` / `''` the caller already computed for
 * `SiteLinkContext`. `path` may carry a query of its own (`/?install=1`), so
 * the separator is chosen rather than assumed.
 */
export function dashboardHref(path: string, langQuery: string): string {
  if (!langQuery) return `${SITE.dashboard}${path}`;
  const separator = path.includes('?') ? '&' : '?';
  return `${SITE.dashboard}${path}${separator}${langQuery.slice(1)}`;
}

/**
 * The console's locale switch, as `.bb-lang-switch` wants it: one link per
 * locale, pointing at the page you are on with `?lang=` set.
 *
 * Links rather than the account form (LangSwitch.svelte): the library's switch
 * is a row of anchors and it renders INSIDE the nav's action group, ahead of
 * the CTA — a form cannot go there, which is how the public bar once ended up
 * with the switch on the wrong side of "Add to Twitch". The account form stays
 * where it is; it also writes the choice to the account and is rendered outside
 * any nav.
 *
 * `?lang=` is enough because hooks.server.ts pins the parameter to the
 * preference cookie on the way through, so the choice survives the next click.
 * That also means these links MUTATE on GET, so every surface rendering them
 * must turn SvelteKit's hover preloading off (data-sveltekit-preload-data),
 * or hovering FR would quietly switch the visitor's language.
 *
 * Marketing does not use this: its locales are path prefixes, so its options
 * come from i18n/ui.ts localeOptions() against the localized routes.
 */
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
