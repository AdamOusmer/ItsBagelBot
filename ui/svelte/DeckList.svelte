<script lang="ts" generics="T extends keyof SvelteHTMLElements = 'div'">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/card.css';
  import '../styles/elements/deck-list.css';
  import type { Snippet } from 'svelte';

  type Own = {
    as?: T;
    class?: string;
    children?: Snippet;
  };

  let {
    as = 'div' as T,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements[T], keyof Own> = $props();

  const classes = $derived(
    ['bb-card', 'bb-deck-list', className || null].filter(Boolean).join(' '),
  );
</script>

<svelte:element this={as} class={classes} {...rest}>{#if children}{@render children()}{/if}</svelte:element>
