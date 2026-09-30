<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  type Own = {
    cols?: 1 | 2 | 3 | 4 | 5 | 6;
    gap?: 1 | 2 | 3 | 4 | 5 | 6;
    min?: string;
    stackAt?: 'sm' | 'md';
    as?: string;
    class?: string;
    children: Snippet;
  };

  let {
    cols = 1,
    gap = 4,
    min,
    stackAt = 'sm',
    as: tag = 'div',
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(
    [
      'bb-grid',
      min ? 'bb-grid--auto' : `bb-grid--${cols}`,
      `bb-grid--gap-${gap}`,
      stackAt === 'md' ? 'bb-grid--stack-md' : null,
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
