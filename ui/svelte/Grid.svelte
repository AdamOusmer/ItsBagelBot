<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  let {
    cols = 1,
    gap = 4,
    min,
    as: tag = 'div',
    class: className = '',
    children,
    ...rest
  }: {
    cols?: 1 | 2 | 3 | 4 | 5 | 6;
    gap?: 1 | 2 | 3 | 4 | 5 | 6;
    min?: string;
    as?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-grid',
      min ? 'bb-grid--auto' : `bb-grid--${cols}`,
      `bb-grid--gap-${gap}`,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element
  this={tag}
  class={classes}
  style={min ? `--grid-min: ${min};` : undefined}
  {...rest}>{@render children()}</svelte:element
>
