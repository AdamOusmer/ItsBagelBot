<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    focusable = false,
    as: tag = 'span',
    class: className = '',
    children,
    ...rest
  }: {
    focusable?: boolean;
    as?: 'span' | 'div' | 'p' | 'a';
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-sr-only', focusable ? 'bb-sr-only--focusable' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
