<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-deck-list` contract
  // (../styles/elements/deck-list.css). Its Astro twin is
  // ../astro/DeckList.astro; ../test/parity.test.ts holds them together.
  //
  // The container every management deck puts its rows inside. Its only job is
  // guaranteeing identical container padding and class across nine list pages;
  // the rows themselves are supplied by the caller.
  //
  // `as` picks the tag so a deck can stay a landmark: a page that needs a
  // <section> with aria-labelledby must pass as="section", because rendering it
  // as a bare <div> silently drops the labelled region from the a11y tree.
  //
  // It IS a card: the element emits `bb-card` alongside `bb-deck-list`, and
  // deck-list.css contributes only `--card-pad`. Both stylesheets are imported
  // here, in that order, the same way ./NavLink.svelte composes the button.
  import '../styles/elements/card.css';
  import '../styles/elements/deck-list.css';
  import type { Snippet } from 'svelte';

  let {
    as = 'div',
    class: className = '',
    children,
    ...rest
  }: {
    as?: string;
    class?: string;
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-card', 'bb-deck-list', className || null].filter(Boolean).join(' '),
  );
</script>

<svelte:element this={as} class={classes} {...rest}>{#if children}{@render children()}{/if}</svelte:element>
