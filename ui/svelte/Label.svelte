<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  type Own = {
    htmlFor?: string;
    mono?: boolean;
    as?: 'label' | 'span' | 'legend';
    class?: string;
    children: Snippet;
  };

  let {
    htmlFor,
    mono = false,
    as: tag = 'label',
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(
    ['bb-label', mono ? 'bb-label--mono' : null, className || null].filter(Boolean).join(' '),
  );
</script>

<svelte:element this={tag} class={classes} for={tag === 'label' ? htmlFor : undefined} {...rest}
  >{@render children()}</svelte:element
>
