<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import { getUiI18n } from './i18n';

  import '../styles/elements/nav.css';
  import Brand from './Brand.svelte';
  import NavLink from './NavLink.svelte';
  import Hamburger from './Hamburger.svelte';
  import MobileMenu from './MobileMenu.svelte';
  import LanguageSwitcher from './LanguageSwitcher.svelte';
  import { mountHomeLogo, mountTopDownMenu } from '../lib/nav-menu';
  import type { Snippet } from 'svelte';
  import type { UiBrand, UiLocaleOption, UiNavLink } from '../lib/nav-types';

  const i18n = getUiI18n();
  type Own = {
    brand: UiBrand;
    links: UiNavLink[];
    cta?: UiNavLink;
    locales?: UiLocaleOption[];
    localeLabel?: string;
    label?: string;
    menuLabels?: { open?: string; close?: string; panel?: string };
    menuMeta?: string;
    menuId?: string;
    variant?: 'pill' | 'bar';
    menu?: boolean;
    actions?: Snippet;
    mobileFooter?: Snippet;
    class?: string;
  };

  let {
    brand,
    links,
    cta,
    locales,
    localeLabel = i18n.t('nav.language'),
    label = i18n.t('nav.main'),
    menuLabels = {},
    menuMeta,
    menuId = 'bb-mobile-menu',
    variant = 'pill',
    menu = true,
    actions,
    mobileFooter,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['nav'], keyof Own> = $props();

  const classes = $derived(['bb-nav', className || null].filter(Boolean).join(' '));
  const menuOpen = $derived(menuLabels.open ?? i18n.t('nav.menuOpen'));
  const menuClose = $derived(menuLabels.close ?? i18n.t('nav.menuClose'));
  const menuPanel = $derived(menuLabels.panel ?? i18n.t('nav.menu'));

  let navEl = $state<HTMLElement | null>(null);

  $effect(() => {
    const root = navEl?.parentElement;
    if (!root) return;
    const toggle = root.querySelector<HTMLElement>('[data-menu-toggle]');
    const menuEl = root.querySelector<HTMLElement>('[data-mobile-menu]');
    const curvePath = root.querySelector<SVGPathElement>('[data-menu-curve-path]');
    const logo = root.querySelector<HTMLAnchorElement>('[data-home-logo]');

    const disposers: (() => void)[] = [];
    if (toggle && menuEl && curvePath) {
      disposers.push(
        mountTopDownMenu(
          { toggle, menu: menuEl, curvePath },
          { labels: { open: menuOpen, close: menuClose } },
        ),
      );
    }
    if (logo) disposers.push(mountHomeLogo(logo));

    return () => {
      for (const dispose of disposers) dispose();
    };
  });
</script>

<nav
  bind:this={navEl}
  class={classes}
  aria-label={label}
  data-variant={variant}
  {...rest}
  ><div class="bb-nav__inner"
    ><Brand
      class="bb-nav__brand"
      title={brand.title}
      sub={brand.sub}
      href={brand.href}
      logoSrc={brand.logoSrc}
      logoAlt={brand.logoAlt}
      size="sm"
      logoShape={brand.logoShape}
      data-home-logo
    /><ul class="bb-nav__links"
      >{#each links as link (link.href)}<li
          ><NavLink
            href={link.href}
            label={link.label}
            current={link.current}
            external={link.external}
            data-astro-prefetch={link.current || link.external ? undefined : 'hover'}
          /></li
        >{/each}</ul
    ><div class="bb-nav__actions"
      >{#if locales && locales.length > 0}<LanguageSwitcher
          options={locales}
          label={localeLabel}
          menuId="{menuId}-lang"
        />{/if}{#if cta}<NavLink
          class="bb-nav__cta"
          variant="cta"
          href={cta.href}
          label={cta.label}
          external={cta.external}
        />{/if}{#if actions}{@render actions()}{/if}</div
    >{#if menu}<Hamburger
        label={menuOpen}
        closeLabel={menuClose}
        controls={menuId}
      />{/if}</div
  ></nav
>{#if menu}<MobileMenu
    {links}
    {cta}
    id={menuId}
    panelLabel={menuPanel}
    meta={menuMeta}
    footer={mobileFooter}
  />{/if}
