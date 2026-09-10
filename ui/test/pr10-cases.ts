// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The nav/footer/shell parity cases: one entry per element that ships both a
// Svelte and an Astro adapter, with the props each is rendered from.
//
// Separate from parity.test.ts because these cases feed TWO readers: the test,
// which renders both adapters and diffs them against test/__golden__/<name>.html,
// and the one-shot script that produced those files in the first place. The
// script is deliberately not in the repo (a golden that regenerates itself
// proves nothing); this registry is what it was pointed at, and re-pointing a
// fresh copy of it at the same registry is how a deliberate markup change is
// re-baselined.
//
// The props are chosen to exercise the shapes two adapters actually drift on:
// an entry that is current, an entry that is locked (a different ELEMENT plus
// an extra child), a group with children (the collapsing sub-list), a count,
// and an optional prop left off (does each adapter suppress the attribute, or
// emit `aria-current="undefined"`).

import SvelteIcon from '../svelte/Icon.svelte';
import AstroIcon from '../astro/Icon.astro';
import SvelteBrand from '../svelte/Brand.svelte';
import AstroBrand from '../astro/Brand.astro';
import SvelteHamburger from '../svelte/Hamburger.svelte';
import AstroHamburger from '../astro/Hamburger.astro';
import SvelteLanguageSwitcher from '../svelte/LanguageSwitcher.svelte';
import AstroLanguageSwitcher from '../astro/LanguageSwitcher.astro';
import SvelteSocialRail from '../svelte/SocialRail.svelte';
import AstroSocialRail from '../astro/SocialRail.astro';
import SvelteMobileMenu from '../svelte/MobileMenu.svelte';
import AstroMobileMenu from '../astro/MobileMenu.astro';
import SvelteNav from '../svelte/Nav.svelte';
import AstroNav from '../astro/Nav.astro';
import SvelteFooter from '../svelte/Footer.svelte';
import AstroFooter from '../astro/Footer.astro';
import SvelteRailItem from '../svelte/RailItem.svelte';
import AstroRailItem from '../astro/RailItem.astro';
import SvelteRail from '../svelte/Rail.svelte';
import AstroRail from '../astro/Rail.astro';
import SvelteTopbar from '../svelte/Topbar.svelte';
import AstroTopbar from '../astro/Topbar.astro';
import SvelteDock from '../svelte/Dock.svelte';
import AstroDock from '../astro/Dock.astro';
import SveltePageHead from '../svelte/PageHead.svelte';
import AstroPageHead from '../astro/PageHead.astro';

export interface ParityCase {
  /** Also the golden's filename. */
  name: string;
  svelte: unknown;
  astro: unknown;
  props: Record<string, unknown>;
}

const brand = {
  title: 'ItsBagelBot',
  sub: 'DASH',
  href: '/',
  logoSrc: '/logo.png',
};

const links = [
  { href: '/pricing', label: 'Pricing' },
  { href: '/guides', label: 'Guides', active: true },
  { href: 'https://stats.example.test', label: 'Stats', external: true },
];

const locales = [
  { code: 'en', href: '/', label: 'EN', current: true },
  { code: 'fr', href: '/fr/', label: 'FR' },
];

const cta = { href: '/add', label: 'Add to Twitch' };

const groups = [
  {
    label: 'Board',
    items: [
      { href: '/', label: 'Overview', icon: 'overview' as const, active: true },
      {
        href: '/modules',
        label: 'Modules',
        icon: 'modules' as const,
        count: 3,
        children: [{ href: '/modules#chat', label: 'Chat', count: 2 }],
      },
      {
        href: '/loyalty',
        label: 'Loyalty',
        icon: 'users' as const,
        locked: true,
        lockedHint: 'Broadcaster only',
      },
    ],
  },
  { label: 'Ops', items: [{ href: '/audit', label: 'Audit', icon: 'audit' as const, count: 2 }] },
];

export const CASES: ParityCase[] = [
  { name: 'Icon', svelte: SvelteIcon, astro: AstroIcon, props: { name: 'check' } },
  {
    name: 'Brand',
    svelte: SvelteBrand,
    astro: AstroBrand,
    props: { ...brand, premium: true },
  },
  {
    name: 'Hamburger',
    svelte: SvelteHamburger,
    astro: AstroHamburger,
    props: { label: 'Open menu', closeLabel: 'Close menu' },
  },
  {
    name: 'LanguageSwitcher',
    svelte: SvelteLanguageSwitcher,
    astro: AstroLanguageSwitcher,
    props: { options: locales, ariaLabel: 'Language' },
  },
  {
    name: 'SocialRail',
    svelte: SvelteSocialRail,
    astro: AstroSocialRail,
    props: {
      items: [
        { label: 'Discord', href: 'https://discord.example.test', icon: 'discord' },
        { label: 'GitHub', href: 'https://github.example.test', icon: 'github' },
      ],
      ariaLabel: 'Social links',
    },
  },
  {
    name: 'MobileMenu',
    svelte: SvelteMobileMenu,
    astro: AstroMobileMenu,
    props: { links, cta, panelLabel: 'Menu', meta: 'Baked in Montreal' },
  },
  {
    name: 'Nav',
    svelte: SvelteNav,
    astro: AstroNav,
    props: {
      brand,
      links,
      cta,
      locales,
      ariaLabel: 'Primary',
      menuLabels: { open: 'Open menu', close: 'Close menu', panel: 'Menu' },
    },
  },
  {
    name: 'Footer',
    svelte: SvelteFooter,
    astro: AstroFooter,
    props: {
      brand: { ...brand, sub: 'One bot, every stream.' },
      signoff: { line: 'See you on stream.', sub: 'take care' },
      columns: [
        { title: 'Product', links: [{ href: '/pricing', label: 'Pricing' }] },
        {
          title: 'Community',
          links: [{ href: 'https://d.example.test', label: 'Discord', external: true }],
        },
      ],
      legal: [{ href: '/privacy', label: 'Privacy' }],
      copyright: '© 2026 ItsBagelBot',
      note: 'Your data is never sold.',
    },
  },
  {
    name: 'RailItem',
    svelte: SvelteRailItem,
    astro: AstroRailItem,
    props: { href: '/commands', label: 'Commands', icon: 'commands', active: true, count: 4 },
  },
  {
    name: 'RailItemLocked',
    svelte: SvelteRailItem,
    astro: AstroRailItem,
    props: { label: 'Loyalty', icon: 'users', locked: true, lockedHint: 'Broadcaster only' },
  },
  {
    name: 'Rail',
    svelte: SvelteRail,
    astro: AstroRail,
    props: { brand, groups, ariaLabel: 'Board sections' },
  },
  {
    name: 'Topbar',
    svelte: SvelteTopbar,
    astro: AstroTopbar,
    props: {
      brand,
      crumbs: [{ label: 'Board', href: '/' }, { label: 'Commands' }],
      crumbAriaLabel: 'Breadcrumb',
    },
  },
  {
    name: 'Dock',
    svelte: SvelteDock,
    astro: AstroDock,
    props: { groups, ariaLabel: 'Main navigation', fallbackIcon: 'list' },
  },
  {
    name: 'DockFlat',
    svelte: SvelteDock,
    astro: AstroDock,
    props: { items: groups[0].items, ariaLabel: 'Main navigation' },
  },
  {
    name: 'PageHead',
    svelte: SveltePageHead,
    astro: AstroPageHead,
    props: { eyebrow: 'Board', title: 'Commands', description: 'Everything the bot answers to.' },
  },
];
