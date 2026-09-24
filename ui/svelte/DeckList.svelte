<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

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
