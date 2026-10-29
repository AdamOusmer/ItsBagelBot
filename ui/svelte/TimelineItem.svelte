<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/timeline.css';
  import type { Snippet } from 'svelte';

  type Own = {
    dot?: boolean;
    as?: 'li' | 'div';
    class?: string;
    children: Snippet;
  };

  let {
    dot = true,
    as: tag = 'li',
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(['bb-timeline__item', className || null].filter(Boolean).join(' '));
</script>

<svelte:element this={tag} class={classes} {...rest}
  >{#if dot}<span class="bb-timeline__dot" aria-hidden="true"></span>{/if}{@render children()}</svelte:element
>
