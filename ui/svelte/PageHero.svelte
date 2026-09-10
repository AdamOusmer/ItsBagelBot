<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-page-hero` contract
  // (../styles/elements/page-hero.css). Its Astro twin is
  // ../astro/PageHero.astro and ../test/parity.test.ts holds the two to the
  // same markup.
  //
  // No decode wiring here, matching the Astro adapter: `data-decode` is a
  // document-wide contract and the scan belongs to the surface. A Svelte page
  // that wants it puts `use:decode` from ./actions.ts on the hero.
  import '../styles/elements/page-hero.css';
  import LightField from './LightField.svelte';

  let {
    eyebrow,
    title,
    description,
    class: className = '',
    ...rest
  }: {
    eyebrow?: string;
    title: string;
    description?: string;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-page-hero', className || null].filter(Boolean).join(' '));
</script>

<header class={classes} {...rest}><LightField /><div
    class="bb-page-hero__glow"
    aria-hidden="true"
  ></div><div class="bb-page-hero__inner"
  >{#if eyebrow}<span class="bb-page-hero__eyebrow">{eyebrow}</span>{/if}<h1
      class="bb-page-hero__title"
      data-decode={title}>{title}</h1
    >{#if description}<p class="bb-page-hero__desc">{description}</p>{/if}</div
  ></header>
