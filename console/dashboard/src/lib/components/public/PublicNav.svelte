<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary and unlicensed. See LICENSE.md.
  // The marketing site's nav (web/src/components/layout/Nav.astro), as it was
  // first converted for routes/user/[userId]: logo, centred link row, CTA — all
  // routed at the live site. Extracted here so every public page wears the same
  // bar. Link labels and targets come from the i18n catalog + links.ts, so a
  // French visitor gets French labels and /fr/ targets.
  import { getI18n } from '@bagel/shared';
  import LangSwitch from '$lib/components/LangSwitch.svelte';
  import NavLink from './NavLink.svelte';
  import { dashHref, webHome, webHref, type PublicNavLink } from './links';

  let {
    links,
    showLang = false
  }: {
    /** Link row; defaults to the set the public channel page ships. */
    links?: PublicNavLink[];
    /** Render the EN/FR toggle beside the CTA, as the marketing nav does. */
    showLang?: boolean;
  } = $props();

  const { t, locale } = getI18n();

  // Default link row mirrors web's nav: Pricing, Guides, Contact.
  const items = $derived(
    links ?? [
      { href: webHref('/pricing', locale), label: t('public.nav.pricing') },
      { href: webHref('/guides', locale), label: t('public.nav.guides') },
      { href: webHref('/contact', locale), label: t('public.nav.contact') }
    ]
  );
</script>

<nav class="site-nav" aria-label={t('public.nav.aria')}>
  <div class="site-nav__inner">
    <a class="logo" href={webHome(locale)} aria-label={t('public.nav.home')}>
      <img src="/logo.png" alt="" width="35" height="35" />
      <span>ItsBagelBot</span>
    </a>

    <ul class="links" aria-label={t('public.nav.aria')}>
      {#each items as link (link.href)}
        <li><NavLink href={link.href} label={link.label} active={link.active ?? false} /></li>
      {/each}
    </ul>

    <div class="nav-cta">
      {#if showLang}<LangSwitch />{/if}
      <a class="cta-btn" href={dashHref(locale)} target="_blank" rel="noopener noreferrer"
        >{t('public.nav.cta')}</a
      >
    </div>
  </div>
</nav>

<style>
  .site-nav {
    font-family: var(--bb-font-display);
    position: fixed;
    top: 0;
    left: 0;
    width: 100%;
    height: calc(76px + env(safe-area-inset-top, 0px));
    padding-top: env(safe-area-inset-top, 0px);
    padding-inline: max(24px, 4vw);
    border-bottom: 1px solid var(--bb-border);
    z-index: 50;
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    background: rgba(10, 10, 10, 0.7);
  }
  .site-nav__inner {
    display: grid;
    grid-template-columns: minmax(170px, 1fr) minmax(0, auto) minmax(170px, 1fr);
    align-items: center;
    gap: 24px;
    width: min(100%, 1180px);
    height: 100%;
    margin: 0 auto;
  }
  .logo {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
    width: max-content;
    color: var(--bb-white);
    text-decoration: none;
  }
  .logo img { width: 35px; height: 35px; border-radius: 8px; }
  .logo span {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 1.2rem;
    color: var(--bb-white);
    white-space: nowrap;
  }
  ul.links {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 30px;
    min-width: 0;
    padding: 0 1.5rem;
    list-style: none;
    margin: 0;
  }
  ul.links li { display: flex; align-items: center; }
  .nav-cta { display: flex; justify-content: flex-end; align-items: center; gap: 14px; }

  /* CTA — ported from SecondaryButton.astro */
  .cta-btn {
    font-family: var(--bb-font-mono);
    font-size: 0.78rem;
    padding: 10px 22px;
    background: transparent;
    border: 1px solid var(--bb-tan);
    color: var(--bb-tan-light);
    border-radius: 4px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    text-decoration: none;
    white-space: nowrap;
    display: inline-block;
    transition: background 0.2s, color 0.2s;
  }
  .cta-btn:hover { background: var(--bb-tan); color: var(--bb-black); }

  @media (max-width: 1120px) {
    .site-nav__inner { grid-template-columns: minmax(155px, 1fr) minmax(0, auto) minmax(150px, 1fr); gap: 18px; }
    ul.links { gap: 20px; padding-inline: 1rem; }
    .logo span { font-size: 1.08rem; }
  }
  @media (max-width: 900px) {
    .site-nav__inner { grid-template-columns: 1fr auto; gap: 16px; }
    ul.links { display: none; }
  }
</style>
