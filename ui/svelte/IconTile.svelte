<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import type { Snippet } from 'svelte';
  import '../styles/elements/icon-tile.css';

  type Own = {
    size?: 'sm' | 'md' | 'lg';
    tone?: 'success';
    picked?: boolean;
    class?: string;
    children?: Snippet;
  };

  let {
    size = 'md',
    tone,
    picked = false,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLSpanElement>, keyof Own> = $props();

  const classes = $derived(
    [
      'bb-icon-tile',
      size === 'md' ? null : `bb-icon-tile--${size}`,
      tone ? `bb-icon-tile--${tone}` : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<span class={classes} data-picked={picked ? '' : undefined} aria-hidden="true" {...rest}>{@render children?.()}</span>
