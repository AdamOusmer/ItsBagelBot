<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    as: tag = 'p',
    class: className = '',
    children,
    ...rest
  }: {
    as?: 'p' | 'div';
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-lead', className || null].filter(Boolean).join(' '));
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
