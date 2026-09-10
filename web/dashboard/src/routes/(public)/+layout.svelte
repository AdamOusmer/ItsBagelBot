<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // The signed-out surface: the marketing site's chrome around a
  // dashboard-rendered page. Deliberately NO robots noindex (unlike (app)) —
  // these pages are meant to be found.
  //
  // The bar and the sign-off are @bagel/ui's `Nav` and `Footer`, rendered HERE
  // rather than through a pair of console-local wrappers. There used to be
  // PublicNav.svelte and PublicFooter.svelte beside this file, and before them
  // hand-converted copies of the marketing markup under their own `.site-nav` /
  // `.site-footer` class names. Every layer of that was a second implementation
  // of a component the library already ships, and each one drifted: a shorter
  // link row on /login, a footer that filed GitHub under Company, a mobile menu
  // with none of the panel choreography ui/lib/nav-menu.ts owns. The library
  // renders the chrome; this file supplies the two things the library refuses to
  // know, the copy (i18n) and the destinations (@bagel/kit/site-links), and
  // those destinations are the SAME list the marketing site resolves in
  // web/marketing/src/layouts/Layout.astro.
  //
  // Every public page renders through here, so no page can ship a second bar:
  // /login and /user/[channel] each used to draw their own and now live under
  // this group instead.
  import { page } from '$app/state';
  import { Footer, LanguageSwitcher, Nav, getI18n } from '@bagel/kit';
  import { LOCALES } from '@bagel/kit/i18n';
  import { reveal } from '@bagel/ui/svelte/actions';
  import {
    SITE,
    SITE_FOOTER,
    SITE_LEGAL,
    SITE_NAV,
    dashboardHref,
    localeOptions,
    resolveSiteColumns,
    resolveSiteLinks,
    type SiteLinkContext
  } from '@bagel/kit/site-links';

  let { children } = $props();

  const { t, locale } = getI18n();

  /**
   * Marketing paths resolve to ABSOLUTE marketing URLs: from the console every
   * one of these links leaves the app, and the console has no /<locale> routes
   * to hang a relative path off. Mirrors the marketing site's localizePath().
   */
  const webPath = $derived((path: string) =>
    locale === 'en' ? `${SITE.web}${path}` : `${SITE.web}/${locale}${path}`
  );

  const langQuery = $derived(locale === 'en' ? '' : `?lang=${locale}`);

  /**
   * The one entry that can be a local route: this app answers /stats on the
   * stats and dashboard hosts. The href stays the canonical absolute URL (a
   * relative one is what once made leaderboard.itsbagelbot.com/stats a 404 into
   * the [user] route), so "you are here" is decided on the pathname instead.
   */
  const isActive = $derived((href: string) =>
    href === SITE.stats ? page.url.pathname === '/stats' : false
  );

  const navContext = $derived({
    path: webPath,
    label: (key: string) => t(`public.nav.${key}`),
    langQuery,
    isActive
  } satisfies SiteLinkContext);

  const footerContext = $derived({
    path: webPath,
    label: (key: string) => t(`public.footer.${key}`),
    langQuery
  } satisfies SiteLinkContext);

  const links = $derived(resolveSiteLinks(SITE_NAV, navContext));
  const columns = $derived(resolveSiteColumns(SITE_FOOTER, footerContext));
  const legal = $derived(resolveSiteLinks(SITE_LEGAL, footerContext));
  const locales = $derived(localeOptions(LOCALES, locale, page.url));
</script>

<!-- Preloading off for the whole bar: the locale links carry `?lang=`, which
     hooks.server.ts pins to the preference cookie, so SvelteKit's hover
     preload would switch the visitor's language before they clicked. -->
<Nav
  brand={{
    title: 'ItsBagelBot',
    href: locale === 'en' ? SITE.web : `${SITE.web}/${locale}/`,
    logoSrc: '/logo.png',
    logoAlt: ''
  }}
  {links}
  cta={{ href: dashboardHref('/auth/login', langQuery), label: t('public.nav.cta') }}
  {locales}
  localeLabel={t('lang.switchAria')}
  ariaLabel={t('public.nav.aria')}
  menuLabels={{
    open: t('public.nav.menuOpen'),
    close: t('public.nav.menuClose'),
    panel: t('public.nav.menu')
  }}
  menuMeta={t('public.nav.menuMeta')}
  data-sveltekit-preload-data="off"
>
  {#snippet mobileFooter()}
    <a class="menu-app" href={dashboardHref('/?install=1', langQuery)}>{t('public.nav.getApp')}</a>
    <div class="menu-lang" data-sveltekit-preload-data="off">
      <LanguageSwitcher options={locales} ariaLabel={t('lang.switchAria')} />
    </div>
  {/snippet}
</Nav>

{@render children()}

<!-- `--bb-footer-bg` because these pages run fixed background canvases (aurora,
     starfield) the whole way down and the strip has to occlude them; the
     contract is transparent by default, which is what every other consumer
     wants.

     `use:reveal` because `.bb-footer` ships its sign-off, brand and columns as
     `[data-reveal]`, and reveal.css starts those at opacity 0 with a 24px
     offset. The marketing site scans the whole document once per navigation, so
     its footer appears; the console has no such scan, so the library footer
     rendered its full markup at opacity 0 — a page-height band of nothing where
     the links are. -->
<div class="footer-ground" use:reveal>
  <Footer
    brand={{
      title: 'ItsBagelBot',
      sub: t('public.footer.tagline'),
      logoSrc: '/logo.png',
      logoAlt: t('public.footer.logoAlt')
    }}
    signoff={{ line: t('public.footer.signoff'), sub: t('public.footer.signoffSub') }}
    {columns}
    {legal}
    copyright={t('public.footer.copyright')}
    note={t('public.footer.note')}
    style="--bb-footer-bg: var(--bb-bg-0);"
  />
</div>

<style>
  /* The two rows this app adds to the library's panel. Slotted markup carries
     the scope attribute of the file it is WRITTEN in — this one — so these
     rules reach it inside .bb-mobile-menu. Same shape and same reason as the
     marketing site's block in Layout.astro. */
  .menu-app {
    display: block;
    text-align: center;
    font-family: var(--bb-font-mono);
    font-size: 0.78rem;
    letter-spacing: 0.02em;
    text-transform: uppercase;
    color: var(--bb-tan-light);
    text-decoration: none;
    padding: 14px 0;
  }

  .menu-lang {
    display: flex;
    justify-content: center;
    padding-bottom: 10px;
  }
</style>
