<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/nav.css';
  import Brand from './Brand.svelte';
  import NavLink from './NavLink.svelte';
  import Hamburger from './Hamburger.svelte';
  import MobileMenu from './MobileMenu.svelte';
  import LanguageSwitcher from './LanguageSwitcher.svelte';
  import { mountHomeLogo, mountTopDownMenu } from '../lib/nav-menu';
  import type { Snippet } from 'svelte';
  import type { UiBrand, UiLocaleOption, UiNavLink } from '../lib/nav-types';

  let {
    brand,
    links,
    cta,
    locales,
    localeLabel = 'Language',
    ariaLabel,
    menuLabels,
    menuMeta,
    menuId = 'bb-mobile-menu',
    variant = 'pill',
    menu = true,
    actions,
    mobileFooter,
    class: className = '',
    ...rest
  }: {
    brand: UiBrand;
    links: UiNavLink[];
    cta?: UiNavLink;
    locales?: UiLocaleOption[];
    localeLabel?: string;
    ariaLabel: string;
    menuLabels: { open: string; close: string; panel: string };
    menuMeta?: string;
    menuId?: string;
    variant?: 'pill' | 'bar';
    menu?: boolean;
    actions?: Snippet;
    mobileFooter?: Snippet;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-nav', className || null].filter(Boolean).join(' '));

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
          { labels: { open: menuLabels.open, close: menuLabels.close } },
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
  aria-label={ariaLabel}
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
      premium={brand.premium}
      data-home-logo
    /><ul class="bb-nav__links"
      >{#each links as link (link.href)}<li
          ><NavLink
            href={link.href}
            label={link.label}
            current={link.active}
            external={link.external}
          /></li
        >{/each}</ul
    ><div class="bb-nav__actions"
      >{#if locales && locales.length > 0}<LanguageSwitcher
          options={locales}
          ariaLabel={localeLabel}
        />{/if}{#if cta}<NavLink
          class="bb-nav__cta"
          variant="cta"
          href={cta.href}
          label={cta.label}
          external={cta.external}
        />{/if}{#if actions}{@render actions()}{/if}</div
    >{#if menu}<Hamburger
        label={menuLabels.open}
        closeLabel={menuLabels.close}
        controls={menuId}
      />{/if}</div
  ></nav
>{#if menu}<MobileMenu
    {links}
    {cta}
    id={menuId}
    panelLabel={menuLabels.panel}
    meta={menuMeta}
    footer={mobileFooter}
  />{/if}
