<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-nav`. Its Astro twin is ../astro/Nav.astro and
  // ../test/parity.test.ts diffs the two, so the element order, the class
  // lists and the attribute order below are the contract.
  //
  // The engine is attached in an `$effect` rather than by an `astro:page-load`
  // listener, and torn down by its return: that is the entire difference
  // between the two adapters, and it is why the panel choreography lives in
  // ../lib/nav-menu.ts as a plain mount/dispose pair instead of inside either
  // one of them.
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
    /** The one filled entry, in the bar and repeated in the panel. */
    cta?: UiNavLink;
    /** Omit entirely on a single-locale surface; the switch disappears. */
    locales?: UiLocaleOption[];
    localeLabel?: string;
    ariaLabel: string;
    /** Every string the hamburger and the panel need. No copy lives in ui. */
    menuLabels: { open: string; close: string; panel: string };
    /** Panel fine print. */
    menuMeta?: string;
    /** Panel id; the hamburger's aria-controls and the clip id derive from it. */
    menuId?: string;
    /** pill = the floating marketing bar, bar = a full-width docs header. */
    variant?: 'pill' | 'bar';
    /** The hamburger and its panel. Off for a host that already ships a mobile
        menu of its own, where a second one is two hamburgers side by side
        opening two different navigations. */
    menu?: boolean;
    actions?: Snippet;
    mobileFooter?: Snippet;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-nav', className || null].filter(Boolean).join(' '));

  let navEl = $state<HTMLElement | null>(null);

  // Queried out of the rendered tree rather than bound with `bind:this` on
  // every part: the panel and its curve are two components down, and threading
  // three element bindings back up would put the engine's wiring into
  // MobileMenu's props, where a caller could break it by rendering the panel
  // itself. The engine's contract is the data-attributes.
  $effect(() => {
    const root = navEl?.parentElement;
    if (!root) return;
    const toggle = root.querySelector<HTMLElement>('[data-menu-toggle]');
    // `menuEl`, not `menu`: the prop of that name is the on/off switch for
    // this whole feature, and shadowing it here would read as the element.
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
