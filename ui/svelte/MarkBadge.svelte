<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import type { Snippet } from 'svelte';
  import '../styles/elements/mark-badge.css';

  type Own = {
    src?: string;
    alt?: string;
    size?: 'xs' | 'sm' | 'md' | 'lg';
    tone?: 'premium' | 'success';
    class?: string;
    children?: Snippet;
  };

  let {
    src,
    alt = '',
    size = 'md',
    tone,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLSpanElement>, keyof Own> = $props();

  const classes = $derived(
    [
      'bb-mark-badge',
      size === 'md' ? null : `bb-mark-badge--${size}`,
      tone ? `bb-mark-badge--${tone}` : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<span class={classes} {...rest}>{#if src}<img {src} {alt} />{/if}{@render children?.()}</span>
