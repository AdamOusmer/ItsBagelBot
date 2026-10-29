<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/round-badge.css';
  import type { Snippet } from 'svelte';

  type Own = {
    size?: 'xs' | 'sm' | 'md' | 'lg';
    solid?: boolean;
    muted?: boolean;
    as?: 'span' | 'i' | 'b';
    class?: string;
    children: Snippet;
  };

  let {
    size = 'sm',
    solid = false,
    muted = false,
    as: tag = 'span',
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(
    [
      'bb-round-badge',
      size === 'sm' ? null : `bb-round-badge--${size}`,
      solid ? 'bb-round-badge--solid' : null,
      muted ? 'bb-round-badge--muted' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
