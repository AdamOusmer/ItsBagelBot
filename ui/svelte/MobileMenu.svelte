<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import { getUiI18n } from './i18n';
  import '../styles/elements/nav.css';
  import NavLink from './NavLink.svelte';
  import type { Snippet } from 'svelte';
  import type { UiNavLink } from '../lib/nav-types';

  const i18n = getUiI18n();
  type Own = {
    links: UiNavLink[];
    cta?: UiNavLink;
    id?: string;
    panelLabel?: string;
    meta?: string;
    footer?: Snippet;
    class?: string;
  };

  let {
    links,
    cta,
    id = 'bb-mobile-menu',
    panelLabel = i18n.t('nav.menu'),
    meta,
    footer,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const classes = $derived(['bb-mobile-menu', className || null].filter(Boolean).join(' '));

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
            current={link.current}
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
