<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-mobile-menu`. Its Astro twin is
  // ../astro/MobileMenu.astro; ../test/parity.test.ts diffs the two.
  //
  // Renders CLOSED: aria-hidden, inert, and a `d=""` curve. Everything that
  // moves is ../lib/nav-menu.ts, which finds the panel by `data-mobile-menu`,
  // the curve by `data-menu-curve-path`, and the things it staggers by
  // `data-menu-item` / `data-menu-footer`. Those five attributes are the
  // contract between the markup and the engine; the classes are the contract
  // between the markup and the CSS.
  import '../styles/elements/nav.css';
  import NavLink from './NavLink.svelte';
  import type { Snippet } from 'svelte';
  import type { UiNavLink } from '../lib/nav-types';

  let {
    links,
    cta,
    id = 'bb-mobile-menu',
    panelLabel,
    meta,
    footer,
    class: className = '',
    ...rest
  }: {
    links: UiNavLink[];
    /** The one filled entry at the bottom of the panel. */
    cta?: UiNavLink;
    /** Also the hamburger's aria-controls; see the clip note below. */
    id?: string;
    /** Accessible name of the dialog. */
    panelLabel: string;
    /** Fine print under the footer rows. */
    meta?: string;
    /** Extra footer rows: a store link, a locale switch. */
    footer?: Snippet;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-mobile-menu', className || null].filter(Boolean).join(' '));

  // The clipPath id is derived from the instance id rather than fixed. Two
  // panels on one page (a docs header plus an embedded preview) sharing one
  // <clipPath> would animate as one, and the browser picks whichever is first
  // in the document -- a bug that only appears on the page that adds the
  // second one. The CSS reads it as a custom property; see nav.css.
  const clipId = $derived(`${id}-clip`);
</script>

<div
  class={classes}
  {id}
  data-mobile-menu=""
  aria-hidden="true"
  inert
  style="--menu-clip:url(#{clipId})"
  {...rest}
  ><svg class="bb-mobile-menu__curve" aria-hidden="true" focusable="false"
    ><defs
      ><clipPath id={clipId} clipPathUnits="userSpaceOnUse"
        ><path class="bb-mobile-menu__curve-path" data-menu-curve-path d=""></path></clipPath
      ></defs
    ></svg
  ><div class="bb-mobile-menu__panel" role="dialog" aria-label={panelLabel} aria-modal="true"
    ><ul class="bb-mobile-menu__links"
      >{#each links as link (link.href)}<li data-menu-item
          ><NavLink
            class="bb-mobile-menu__link"
            href={link.href}
            label={link.label}
            current={link.active}
            external={link.external}
            block
          /></li
        >{/each}</ul
    ><div class="bb-mobile-menu__footer" data-menu-footer
      >{#if cta}<NavLink
          class="bb-mobile-menu__cta"
          variant="cta"
          href={cta.href}
          label={cta.label}
          external={cta.external}
          block
        />{/if}{#if footer}{@render footer()}{/if}{#if meta}<span
          class="bb-mobile-menu__meta">{meta}</span
        >{/if}</div
    ></div
  ></div
>
