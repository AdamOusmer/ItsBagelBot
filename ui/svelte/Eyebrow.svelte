<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    tone = 'default',
    as: tag = 'span',
    class: className = '',
    children,
    ...rest
  }: {
    tone?: 'default' | 'go';
    as?: 'span' | 'p' | 'div';
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-eyebrow', tone === 'default' ? null : `bb-eyebrow--${tone}`, className || null].filter(Boolean).join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
