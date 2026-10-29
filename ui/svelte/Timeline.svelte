<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/timeline.css';
  import type { Snippet } from 'svelte';

  type Own = {
    rail?: 'fade' | 'bridge';
    draw?: boolean;
    as?: 'ol' | 'ul' | 'div';
    class?: string;
    children: Snippet;
  };

  let {
    rail = 'fade',
    draw = false,
    as: tag = 'ol',
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(
    [
      'bb-timeline',
      rail === 'fade' ? null : `bb-timeline--${rail}`,
      draw ? 'bb-timeline--draw' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
