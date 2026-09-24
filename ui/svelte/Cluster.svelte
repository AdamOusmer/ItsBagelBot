<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  let {
    gap = 2,
    justify,
    align,
    nowrap = false,
    as: tag = 'div',
    class: className = '',
    children,
    ...rest
  }: {
    gap?: 1 | 2 | 3 | 4 | 5 | 6;
    justify?: 'start' | 'center' | 'end' | 'between';
    align?: 'baseline' | 'stretch';
    nowrap?: boolean;
    as?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-cluster',
      `bb-cluster--${gap}`,
      justify ? `bb-cluster--${justify}` : null,
      align ? `bb-cluster--${align}` : null,
      nowrap ? 'bb-cluster--nowrap' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
