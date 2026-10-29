<script lang="ts" generics="T extends keyof SvelteHTMLElements = 'div'">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/well.css';
  import type { Snippet } from 'svelte';

  type Own = {
    container?: boolean;
    as?: T;
    class?: string;
    children?: Snippet;
  };

  let {
    container = false,
    as: tag = 'div' as T,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements[T], keyof Own> = $props();

  const classes = $derived(
    ['bb-well', container ? 'bb-well--container' : null, className || null].filter(Boolean).join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children?.()}</svelte:element>
