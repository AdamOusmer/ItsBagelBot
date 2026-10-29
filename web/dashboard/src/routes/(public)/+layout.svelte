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
    SITE_FOOTER,
    SITE_LEGAL,
    SITE_NAV,
    dashboardHref,
    localeOptions,
    resolveSiteColumns,
    linkLocale,
    resolveSiteLinks,
    siteNavProps,
    webHref,
    type SiteLinkContext
  } from '@bagel/kit/site-links';

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

  const navProps = $derived(
    siteNavProps({
      home: locale === 'en' ? SITE.web : `${SITE.web}/${locale}/`,
      logoSrc: '/logo.png',
      links: resolveSiteLinks(SITE_NAV, navContext),
      ctaHref: dashboardHref('/auth/login', langQuery),
      text: {
        cta: t('public.nav.cta'),
        aria: t('public.nav.aria'),
        menuOpen: t('public.nav.menuOpen'),
        menuClose: t('public.nav.menuClose'),
        menu: t('public.nav.menu'),
        menuMeta: t('public.nav.menuMeta')
      }
    })
  );
  const columns = $derived(resolveSiteColumns(SITE_FOOTER, footerContext));
  const legal = $derived(resolveSiteLinks(SITE_LEGAL, footerContext));
  const locales = $derived(
    localeOptions(LOCALES, locale, page.url, (code) => page.data.localeNames?.[code] ?? code)
  );
</script>

<Nav
  {...navProps}
  {locales}
  localeLabel={t('lang.switchAria')}
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
    studio={t('public.footer.studio')}
  />
</div>

<style>
  .footer-ground {
    background: var(--bb-bg-0);
  }

  .menu-lang {
    display: flex;
    justify-content: center;
  }
</style>
