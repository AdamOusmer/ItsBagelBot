<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { page } from '$app/state';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Footer from '@bagel/ui/svelte/Footer.svelte';
  import LanguageSwitcher from '@bagel/ui/svelte/LanguageSwitcher.svelte';
  import Nav from '@bagel/ui/svelte/Nav.svelte';
  import { getI18n } from '@bagel/kit';
  import { LOCALES } from '@bagel/kit/i18n';
  import { reveal } from '@bagel/ui/svelte/actions';
  import {
    SITE,
    SITE_NAV,
    dashboardHref,
    localeOptions,
    linkLocale,
    resolveSiteLinks,
    webHref,
    type SiteLinkContext
  } from '@bagel/kit/site-links';
  import { siteFooterProps } from '@bagel/kit/site-footer';

  let { children } = $props();

  const { t, locale } = getI18n();

  const webPath = $derived((path: string, only?: readonly string[]) => webHref(linkLocale(locale, only), path));

  const langQuery = $derived(locale === 'en' ? '' : `?lang=${locale}`);

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
  const footer = $derived(siteFooterProps(footerContext, '/logo.png'));
  const locales = $derived(
    localeOptions(LOCALES, locale, page.url, (code) => page.data.localeNames?.[code] ?? code)
  );
</script>

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
  label={t('public.nav.aria')}
  menuLabels={{
    open: t('public.nav.menuOpen'),
    close: t('public.nav.menuClose'),
    panel: t('public.nav.menu')
  }}
  menuMeta={t('public.nav.menuMeta')}
  data-sveltekit-preload-data="off"
>
  {#snippet mobileFooter()}
    <ButtonLink variant="ghost" block href={dashboardHref('/?install=1', langQuery)}>{t('public.nav.getApp')}</ButtonLink>
    <div class="menu-lang" data-sveltekit-preload-data="off">
      <LanguageSwitcher options={locales} label={t('lang.switchAria')} variant="field" />
    </div>
  {/snippet}
</Nav>

{@render children()}

<div use:reveal>
  <Footer {...footer} />
</div>

<style>
  .menu-lang {
    display: flex;
    justify-content: center;
    padding-bottom: 10px;
  }
</style>
