<script lang="ts" generics="T extends keyof SvelteHTMLElements = 'div'">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  type Own = {
    gap?: 0 | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8;
    align?: 'start' | 'center' | 'end';
    as?: T;
    class?: string;
    children: Snippet;
  };

  let {
    gap = 4,
    align,
    as: tag = 'div' as T,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements[T], keyof Own> = $props();

  const classes = $derived(
    ['bb-stack', `bb-stack--${gap}`, align ? `bb-stack--${align}` : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
