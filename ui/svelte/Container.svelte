<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  type Own = {
    width?: 'default' | 'narrow' | 'text';
    flush?: boolean;
    as?: string;
    class?: string;
    children: Snippet;
  };

  let {
    width = 'default',
    flush = false,
    as: tag = 'div',
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

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
