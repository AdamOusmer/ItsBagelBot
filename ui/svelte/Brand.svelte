<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-brand` contract. Its Astro twin is
  // ../astro/Brand.astro; ../test/parity.test.ts diffs the two.
  //
  // Renders an <a> when it links somewhere and a <div> when it does not,
  // because the console rail's call sign is a label, not a control: wrapping
  // it in an anchor to nowhere put a focus stop in front of the nav for every
  // keyboard user.
  import '../styles/elements/brand-mark.css';

  let {
    title,
    sub,
    href,
    logoSrc,
    logoAlt = '',
    size = 'md',
    premium = false,
    class: className = '',
    ...rest
  }: {
    /** The name. Always rendered. */
    title: string;
    /** The line under it: a station id, a board name, a tagline. */
    sub?: string;
    /** Makes the block a link. Omit for a label. */
    href?: string;
    /** Pre-resolved URL. ui never imports an asset; see brand-mark.css. */
    logoSrc?: string;
    /** Empty by default: next to the name, the mark is decorative. */
    logoAlt?: string;
    /** sm = bar/strip (26px), md = rail (30px), lg = footer (55px). */
    size?: 'sm' | 'md' | 'lg';
    /** Rounds the mark to a circle. The only premium mark in the chrome. */
    premium?: boolean;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-brand', `bb-brand--${size}`, className || null].filter(Boolean).join(' '),
  );

  // Written to width/height so the mark reserves its box before the image
  // decodes; the CSS sizes it for real (a caller may scale --brand-mark).
  const px = $derived(size === 'sm' ? 26 : size === 'lg' ? 55 : 30);
</script>

{#if href}
  <a class={classes} {href} data-premium={premium ? '' : undefined} {...rest}
    >{#if logoSrc}<span class="bb-brand__logo"
        ><img src={logoSrc} alt={logoAlt} width={px} height={px} /></span
      >{/if}<span class="bb-brand__id"
      ><span class="bb-brand__name">{title}</span>{#if sub}<span
          class="bb-brand__sub">{sub}</span
        >{/if}</span
    ></a
  >
{:else}
  <div class={classes} data-premium={premium ? '' : undefined} {...rest}
    >{#if logoSrc}<span class="bb-brand__logo"
        ><img src={logoSrc} alt={logoAlt} width={px} height={px} /></span
      >{/if}<span class="bb-brand__id"
      ><span class="bb-brand__name">{title}</span>{#if sub}<span
          class="bb-brand__sub">{sub}</span
        >{/if}</span
    ></div
  >
{/if}
