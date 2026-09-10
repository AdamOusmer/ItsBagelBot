<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-section-heading` contract
  // (../styles/elements/section-heading.css). Astro twin:
  // ../astro/SectionHeading.astro.
  //
  // The badge rides the eyebrow line rather than the title; the reason is in
  // the contract file. The meta row renders when there is an eyebrow OR a
  // badge, so a heading that is only a badge still gets the 14px above the
  // title.
  import '../styles/elements/section-heading.css';
  import type { Snippet } from 'svelte';

  let {
    eyebrow,
    title,
    align = 'center',
    class: className = '',
    badge,
    ...rest
  }: {
    eyebrow?: string;
    title: string;
    align?: 'center' | 'left';
    class?: string;
    badge?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-section-heading', `bb-section-heading--${align}`, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div class={classes} {...rest}>{#if eyebrow || badge}<div
    class="bb-section-heading__meta"
    data-reveal
  >{#if eyebrow}<span class="bb-section-heading__eyebrow">{eyebrow}</span>{/if}{#if badge}{@render badge()}{/if}</div
  >{/if}<h2 class="bb-section-heading__title" data-reveal style="--reveal-i: 1">{title}</h2></div>
