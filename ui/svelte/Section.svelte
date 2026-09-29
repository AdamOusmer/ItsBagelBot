<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  type Own = {
    size?: 'default' | 'sm' | 'lg' | 'flush' | 'page';
    anchor?: boolean;
    reveal?: boolean;
    as?: string;
    class?: string;
    children: Snippet;
  };

  let {
    size = 'default',
    anchor = false,
    reveal = false,
    as: tag = 'section',
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

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
