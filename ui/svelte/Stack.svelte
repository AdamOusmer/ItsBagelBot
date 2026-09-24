<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  let {
    gap = 4,
    align,
    as: tag = 'div',
    class: className = '',
    children,
    ...rest
  }: {
    gap?: 0 | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8;
    align?: 'start' | 'center' | 'end';
    as?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-stack', `bb-stack--${gap}`, align ? `bb-stack--${align}` : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
