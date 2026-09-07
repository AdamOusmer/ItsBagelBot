<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The marketing site's nav (web/src/components/layout/Nav.astro), as it was
  // first converted for routes/user/[channel]: logo, centred link row, CTA (all
  // routed at the live site). Extracted here so every public page wears the same
  // bar. Link labels and targets come from the i18n catalog + links.ts, so a
  // French visitor gets French labels and /fr/ targets.
  //
  // Under 1024px the link row and CTA fold into web's hamburger + top-down
  // menu (MobileMenu.svelte). The open flag lives here; the menu animates it.
  import { onMount } from 'svelte';
  import { getI18n } from '@bagel/shared';
  import LangSwitch from '$lib/components/LangSwitch.svelte';
  import MobileMenu from './MobileMenu.svelte';
  import NavLink from './NavLink.svelte';
  import { dashLoginHref, webHome, webHref, type PublicNavLink } from './links';

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

  const COMPACT = '(max-width: 1023px)';
  let open = $state(false);
  let menu: MobileMenu;

  const close = () => (open = false);

  onMount(() => {
    const compact = window.matchMedia(COMPACT);
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && open) close();
    };
    // Leaving the compact range, or the page itself, snaps the menu shut
    // without the close animation.
    const snap = () => {
      open = false;
      menu?.snapClosed();
    };
    const onViewport = (e: MediaQueryListEvent) => {
      if (!e.matches) snap();
    };
    document.addEventListener('keydown', onKey);
    compact.addEventListener('change', onViewport);
    window.addEventListener('pagehide', snap);
    return () => {
      document.removeEventListener('keydown', onKey);
      compact.removeEventListener('change', onViewport);
      window.removeEventListener('pagehide', snap);
      document.body.classList.remove('nav-menu-open');
    };
  });

  // Scroll lock while the menu covers the page, as web's body.nav-menu-open.
  $effect(() => {
    document.body.classList.toggle('nav-menu-open', open);
  });
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
      <a class="cta-btn" href={dashLoginHref(locale)} target="_blank" rel="noopener noreferrer"
        >{t('public.nav.cta')}</a
      >
    </div>

    <button
      class="hamburger"
      class:is-open={open}
      type="button"
      aria-label={open ? t('public.nav.menuClose') : t('public.nav.menuOpen')}
      aria-expanded={open}
      aria-controls="public-mobile-menu"
      onclick={() => (open = !open)}
    >
      <span></span>
      <span></span>
      <span></span>
    </button>
  </div>
</nav>

<MobileMenu bind:this={menu} {open} links={items} ctaHref={dashLoginHref(locale)} {showLang} onclose={close} />

<style>
  /* Floating pill nav, mirroring the marketing site's Nav.astro: transparent
     positioning band that never eats clicks, glass pill inside. */
  .site-nav {
    font-family: var(--bb-font-display);
    position: fixed;
    top: 0;
    left: 0;
    width: 100%;
    padding-top: calc(14px + env(safe-area-inset-top, 0px));
    padding-inline: max(16px, env(safe-area-inset-left, 0px), env(safe-area-inset-right, 0px));
    z-index: 50;
    display: flex;
    justify-content: center;
    pointer-events: none;
  }
  .site-nav__inner {
    display: grid;
    grid-template-columns: minmax(150px, 1fr) minmax(0, auto) minmax(150px, 1fr);
    align-items: center;
    gap: 24px;
    width: min(100%, 1000px);
    padding: 9px 12px 9px 18px;
    border: 1px solid var(--bb-border);
    border-radius: 999px;
    background: rgba(10, 10, 10, 0.55);
    backdrop-filter: blur(18px);
    pointer-events: auto;
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
  .logo img { width: 26px; height: 26px; border-radius: 6px; }
  .logo span {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 0.95rem;
    letter-spacing: -0.01em;
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

  /* CTA: the marketing pill nav's green button */
  .cta-btn {
    font-family: var(--bb-font-mono);
    font-size: 0.7rem;
    padding: 9px 18px;
    background: var(--bb-green, #2d6a4f);
    border: 1px solid #40916c;
    color: var(--bb-white);
    border-radius: 999px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    text-decoration: none;
    white-space: nowrap;
    display: inline-block;
    transition: background 0.2s, box-shadow 0.2s;
  }
  .cta-btn:hover {
    background: #40916c;
    box-shadow: 0 0 24px rgba(82, 183, 136, 0.35);
  }

  /* Hamburger (web/src/components/ui/Hamburger.astro): a 44px circle that
     matches the nav pill; three bars fold into a cross while the menu is open. */
  .hamburger {
      display: none;
      width: 44px;
      height: 44px;
      background: rgba(240, 236, 228, 0.05);
      border: 1px solid rgba(240, 236, 228, 0.14);
      border-radius: 999px;
      padding: 0;
      cursor: pointer;
      flex-direction: column;
      justify-content: center;
      align-items: center;
      gap: 4px;
      align-self: center;
      position: relative;
      z-index: 51;
      touch-action: manipulation;
      -webkit-tap-highlight-color: transparent;
      transition: border-color 180ms ease,
                  background 180ms ease,
                  transform 280ms var(--bb-ease-out-expo);
  }

  .hamburger:hover,
  .hamburger:focus-visible {
      border-color: rgba(224, 196, 154, 0.5);
      background: rgba(201, 168, 124, 0.1);
      outline: none;
  }

  .hamburger:active { transform: scale(0.92); }

  .hamburger.is-open {
      border-color: rgba(224, 196, 154, 0.55);
      background: rgba(201, 168, 124, 0.14);
  }

  /* Three pills, the middle one shorter so the glyph reads as a mark rather
     than a grille; they fold into a cross while the menu is open. Bar pitch
     is 6px (2px bar + 4px gap), so the outer bars travel 6px to meet. */
  .hamburger span {
      display: block;
      width: 18px;
      height: 2px;
      background: var(--bb-white);
      border-radius: 999px;
      transform-origin: center;
      transition: transform 280ms var(--bb-ease-out-expo),
                  width 280ms var(--bb-ease-out-expo),
                  opacity 180ms ease;
  }
  .hamburger span:nth-child(2) { width: 12px; }

  .hamburger.is-open span:nth-child(1) { transform: translateY(6px) rotate(45deg); }
  .hamburger.is-open span:nth-child(2) { opacity: 0; width: 0; }
  .hamburger.is-open span:nth-child(3) { transform: translateY(-6px) rotate(-45deg); }

  /* The open menu covers the page; the page must not scroll under it. */
  :global(body.nav-menu-open) { overflow: hidden; }

  @media (max-width: 1120px) {
    .site-nav__inner { grid-template-columns: minmax(140px, 1fr) minmax(0, auto) minmax(140px, 1fr); gap: 16px; }
    ul.links { gap: 18px; padding-inline: 1rem; }
    .cta-btn { padding-inline: 14px; }
  }
  @media (max-width: 1023px) {
    .site-nav__inner { grid-template-columns: 1fr auto; gap: 16px; padding: 7px 10px 7px 16px; }
    ul.links, .nav-cta { display: none; }
    .hamburger { display: flex; }
  }
</style>
