<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The marketing site's mobile menu (web/marketing/src/components/layout/MobileMenu.astro)
  // for the public pages' nav: a full-height panel under the pill that the
  // hamburger drops down, clipped by a curve that flattens as it opens. The
  // motion lives in menu-animator.ts; this file owns the markup and the
  // open/closed accessibility state.
  import { onMount } from 'svelte';
  import { getI18n } from '@bagel/shared';
  import LangSwitch from '$lib/components/LangSwitch.svelte';
  import { MenuAnimator } from './menu-animator';
  import { dashInstallHref, type PublicNavLink } from './links';

  let {
    open,
    links,
    ctaHref,
    showLang = false,
    onclose
  }: {
    open: boolean;
    links: PublicNavLink[];
    ctaHref: string;
    showLang?: boolean;
    onclose: () => void;
  } = $props();

  const { t, locale } = getI18n();

  let menuEl: HTMLDivElement;
  let pathEl: SVGPathElement;
  let animator: MenuAnimator | null = null;

  onMount(() => {
    animator = new MenuAnimator(menuEl, pathEl);
    const onResize = () => animator?.resize();
    window.addEventListener('resize', onResize, { passive: true });
    return () => {
      window.removeEventListener('resize', onResize);
      animator?.destroy();
      animator = null;
    };
  });

  $effect(() => {
    animator?.set(open);
  });

  /** Snap shut with no motion: the viewport left the compact range, or the page is going away. */
  export function snapClosed(): void {
    animator?.set(false, true);
  }
</script>

<div
  class="mobile-menu"
  id="public-mobile-menu"
  bind:this={menuEl}
  aria-hidden={!open}
  inert={!open}
  onclick={(e) => e.target === menuEl && onclose()}
>
  <svg class="curve" aria-hidden="true" focusable="false">
    <defs>
      <clipPath id="public-mobile-menu-clip" clipPathUnits="userSpaceOnUse">
        <path class="curve-path" bind:this={pathEl} d=""></path>
      </clipPath>
    </defs>
  </svg>

  <div class="panel" role="dialog" aria-modal="true" aria-label={t('public.nav.menu')}>
    <ul class="menu-links">
      {#each links as link (link.href)}
        <li data-menu-item><a class="menu-link" href={link.href} onclick={onclose}>{link.label}</a></li>
      {/each}
    </ul>

    <div class="menu-footer" data-menu-footer>
      <a class="menu-cta" href={ctaHref}>{t('public.nav.cta')}</a>
      <a class="menu-app" href={dashInstallHref(locale)}>{t('public.nav.getApp')}</a>
      {#if showLang}<div class="menu-lang"><LangSwitch /></div>{/if}
      <span class="menu-meta">{t('public.nav.menuMeta')}</span>
    </div>
  </div>
</div>

<style>
  .mobile-menu {
    display: none;
    position: fixed;
    inset: calc(var(--bb-nav-height, 76px) + env(safe-area-inset-top, 0px)) 0 0;
    padding: 16px;
    background: rgba(10, 10, 10, 0.72);
    backdrop-filter: blur(22px);
    z-index: 49;
    isolation: isolate;
    overflow: hidden;
    transform: translateY(-100%);
    clip-path: url(#public-mobile-menu-clip);
    will-change: transform;
    visibility: hidden;
    pointer-events: none;
  }
  .mobile-menu:global(.is-visible) { visibility: visible; }
  .mobile-menu:global(.is-open) { pointer-events: auto; }

  .curve {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
  }
  .curve-path { fill: #fff; }

  .panel {
    position: relative;
    z-index: 1;
    width: min(720px, 100%);
    height: 100%;
    margin: 0 auto;
    padding: clamp(34px, 6vw, 56px);
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    gap: 40px;
    overflow-y: auto;
  }

  .menu-links {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .menu-link {
    display: block;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 2.45rem;
    line-height: 1.2;
    color: var(--bb-white);
    text-decoration: none;
    padding: 6px 0;
  }
  .menu-link:active { color: var(--bb-green-glow); }

  /* Items start hidden; the animator drives opacity + translateY inline. */
  .menu-links li,
  .menu-footer {
    opacity: 0;
    transform: translateY(-28px);
    will-change: opacity, transform;
  }

  .menu-footer {
    display: flex;
    flex-direction: column;
    gap: 16px;
    margin-bottom: calc(clamp(56px, 8vh, 86px) + env(safe-area-inset-bottom, 0px));
    padding-top: 24px;
    border-top: 1px solid var(--bb-border);
  }
  .menu-cta {
    display: block;
    text-align: center;
    font-family: var(--bb-font-mono);
    font-size: 0.85rem;
    padding: 16px 24px;
    background: transparent;
    border: 1px solid var(--bb-tan);
    color: var(--bb-tan-light);
    border-radius: var(--bb-radius-sm);
    text-transform: uppercase;
    text-decoration: none;
    transition: background 200ms ease, color 200ms ease;
  }
  .menu-cta:active { background: var(--bb-tan); color: var(--bb-black); }
  .menu-app {
    display: block;
    text-align: center;
    font-family: var(--bb-font-mono);
    font-size: 0.78rem;
    text-transform: uppercase;
    color: var(--bb-tan-light);
    text-decoration: underline;
    text-decoration-color: rgba(201, 168, 124, 0.4);
    text-underline-offset: 4px;
    transition: color 200ms ease;
  }
  .menu-app:active { color: var(--bb-green-glow); }
  .menu-lang { display: flex; justify-content: center; }
  .menu-meta {
    font-family: var(--bb-font-mono);
    font-size: 0.7rem;
    text-transform: uppercase;
    color: var(--bb-muted);
    text-align: center;
  }

  @media (max-width: 1023px) {
    .mobile-menu { display: flex; }
  }
  @media (max-width: 639px) {
    .mobile-menu { padding: 0; background: rgba(10, 10, 10, 0.96); }
    .panel { width: 100%; padding: 32px 24px 28px; }
    .menu-link { font-size: 2.2rem; }
    .menu-footer { margin-bottom: calc(clamp(64px, 9vh, 92px) + env(safe-area-inset-bottom, 0px)); }
  }
  @media (max-width: 380px) {
    .panel { padding: 28px 20px 24px; }
    .menu-link { font-size: 2rem; }
  }
</style>
