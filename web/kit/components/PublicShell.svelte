<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import type { Snippet } from 'svelte';
  import { page } from '$app/state';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Footer from '@bagel/ui/svelte/Footer.svelte';
  import LanguageSwitcher from '@bagel/ui/svelte/LanguageSwitcher.svelte';
  import Nav from '@bagel/ui/svelte/Nav.svelte';
  import ReadingProgress from '@bagel/ui/svelte/ReadingProgress.svelte';
  import { reveal } from '@bagel/ui/svelte/actions';
  import logo320 from '../assets/logo-320.webp';
  import { getI18n } from '../lib/i18n/context';
  import { LOCALES, localeName } from '../lib/i18n/messages';
  import {
    SITE,
    SITE_NAV,
    dashboardHref,
    localeOptions,
    linkLocale,
    resolveSiteLinks,
    siteNavProps,
    webHref,
    type SiteLinkContext
  } from '../lib/site-links';
  import { siteFooterProps } from '../lib/site-footer';

  let {
    children,
    loginPath,
    footer: showFooter = true
  }: { children: Snippet; loginPath?: string; footer?: boolean } = $props();

  const { t, locale } = getI18n();

  const webPath = $derived((path: string, only?: readonly string[]) => webHref(linkLocale(locale, only), path));

  const langQuery = $derived(locale === 'en' ? '' : `?lang=${locale}`);

  const loginHref = $derived(loginPath ? `${loginPath}${langQuery}` : dashboardHref('/auth/login', langQuery));

  const navContext = $derived({
    path: webPath,
    label: (key: string) => t(`public.nav.${key}`),
    langQuery,
    url: page.url
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
      ctaHref: loginHref,
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
  const footer = $derived(siteFooterProps(footerContext, logo320));
  const locales = $derived(localeOptions(LOCALES, locale, new URL(page.url.href), localeName));
</script>

<Nav
  {...navProps}
  {locales}
  localeLabel={t('lang.switchAria')}
  data-sveltekit-preload-data="off"
>
  {#snippet mobileFooter()}
    <ButtonLink variant="ghost" block href={dashboardHref('/?install=1', langQuery)}>{t('public.nav.getApp')}</ButtonLink>
    <Cluster justify="center" data-sveltekit-preload-data="off">
      <LanguageSwitcher options={locales} label={t('lang.switchAria')} variant="field" />
    </Cluster>
  {/snippet}
</Nav>
<ReadingProgress />

{@render children()}

{#if showFooter}
  <div use:reveal>
    <Footer {...footer} />
  </div>
{/if}
