<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import type { Snippet } from 'svelte';
  import '../styles/elements/avatar-plate.css';

  type Own = {
    size?: number;
    class?: string;
    children: Snippet;
  };

  let {
    size,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLSpanElement>, keyof Own> = $props();

  const classes = $derived(['bb-avatar-plate', className || null].filter(Boolean).join(' '));
</script>

<span class={classes} style={size === undefined ? undefined : `--avatar-plate-size: ${size}px`} {...rest}>{@render children()}</span>
