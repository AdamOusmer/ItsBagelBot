<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import type { Snippet } from 'svelte';
  import '../styles/elements/action-bar.css';

  type Own = {
    as?: 'div' | 'form';
    roomy?: boolean;
    class?: string;
    children: Snippet;
    actions?: Snippet;
  };

  let {
    as = 'div',
    roomy = false,
    class: className = '',
    children,
    actions,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(
    ['bb-action-bar', roomy ? 'bb-action-bar--roomy' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={as} class={classes} {...rest}>{@render children()}{#if actions}<span class="bb-action-bar__acts">{@render actions()}</span>{/if}</svelte:element>
