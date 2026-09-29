<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  type Own = {
    tone?: 'default' | 'go';
    as?: 'span' | 'p' | 'div';
    class?: string;
    children: Snippet;
  };

  let {
    tone = 'default',
    as: tag = 'span',
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(
    ['bb-eyebrow', tone === 'default' ? null : `bb-eyebrow--${tone}`, className || null].filter(Boolean).join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
