<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  let {
    width = 'default',
    flush = false,
    as: tag = 'div',
    class: className = '',
    children,
    ...rest
  }: {
    width?: 'default' | 'narrow' | 'text';
    flush?: boolean;
    as?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-container',
      width === 'default' ? null : `bb-container--${width}`,
      flush ? 'bb-container--flush' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
