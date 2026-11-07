<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/canvas-card.css';

  type Own = {
    background?: boolean;
    selected?: boolean;
    class?: string;
    children: Snippet;
  };

  let {
    background = false,
    selected = false,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLDivElement>, keyof Own> = $props();

  const classes = $derived(
    ['bb-canvas-card', background ? 'bb-canvas-card--bg' : null, selected ? 'selected' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div class={classes} {...rest}>{@render children()}</div>
