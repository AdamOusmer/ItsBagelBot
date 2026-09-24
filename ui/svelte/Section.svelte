<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  let {
    size = 'default',
    anchor = false,
    reveal = false,
    as: tag = 'section',
    class: className = '',
    children,
    ...rest
  }: {
    size?: 'default' | 'sm' | 'lg' | 'flush';
    anchor?: boolean;
    reveal?: boolean;
    as?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-section',
      size === 'default' ? null : `bb-section--${size}`,
      anchor ? 'bb-section--anchor' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} data-reveal={reveal ? '' : undefined} {...rest}
  >{@render children()}</svelte:element
>
