<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-footer`. Its Astro twin is ../astro/Footer.astro;
  // ../test/parity.test.ts diffs the two.
  //
  // Every string is a prop and every link is a `UiNavLink`, including the
  // legal strip. The `colophon` snippet is the deliberate exception to "the
  // footer renders what it is given": the marketing site's ownership block is
  // bot-specific, homepage-gated and load-bearing for an external ownership
  // scan, so it stays in that site and is passed in here.
  import '../styles/elements/footer.css';
  import Brand from './Brand.svelte';
  import NavLink from './NavLink.svelte';
  import type { Snippet } from 'svelte';
  import type { UiBrand, UiFooterColumn, UiNavLink } from '../lib/nav-types';

  let {
    brand,
    signoff,
    columns = [],
    legal = [],
    copyright,
    note,
    colophon,
    class: className = '',
    ...rest
  }: {
    brand: UiBrand;
    /** The goodbye line and its hand-written sub-line. */
    signoff?: { line: string; sub?: string };
    columns?: UiFooterColumn[];
    legal?: UiNavLink[];
    copyright: string;
    /** The one promise repeated at the bottom. */
    note?: string;
    /** Rendered above the footer proper; see the note on this slot. */
    colophon?: Snippet;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-footer', className || null].filter(Boolean).join(' '));
</script>

{#if colophon}{@render colophon()}{/if}<footer class={classes} {...rest}
  >{#if signoff}<div class="bb-footer__signoff" data-reveal
      ><p class="bb-footer__signoff-line">{signoff.line}</p>{#if signoff.sub}<p
          class="bb-footer__signoff-sub">{signoff.sub}</p
        >{/if}</div
    >{/if}<div class="bb-footer__top"
    ><Brand
      class="bb-footer__brand"
      title={brand.title}
      sub={brand.sub}
      href={brand.href}
      logoSrc={brand.logoSrc}
      logoAlt={brand.logoAlt}
      size="lg"
      data-reveal
    /><div class="bb-footer__cols"
      >{#each columns as column, i (column.title)}<div
          class="bb-footer__col"
          data-reveal
          style="--reveal-i: {i + 1}"
          ><p class="bb-footer__col-title">{column.title}</p>{#each column.links as link (link.href)}<NavLink
              href={link.href}
              label={link.label}
              current={link.active}
              external={link.external}
            />{/each}</div
        >{/each}</div
    ></div
  ><div class="bb-footer__bottom"
    ><span class="bb-footer__copy">{copyright}</span><span class="bb-footer__note"
      >{note}</span
    ><div class="bb-footer__legal"
      >{#each legal as link (link.href)}<NavLink
          href={link.href}
          label={link.label}
          current={link.active}
          external={link.external}
        />{/each}</div
    ></div
  ></footer
>
