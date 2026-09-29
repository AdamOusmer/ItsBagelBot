<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/fact-list.css';
  import type { Snippet } from 'svelte';

  let {
    layout = 'rows',
    class: className = '',
    children,
    ...rest
  }: {
    layout?: 'rows' | 'tiles' | 'inline';
    class?: string;
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-facts', layout === 'rows' ? null : `bb-facts--${layout}`, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<dl class={classes} {...rest}>{#if children}{@render children()}{/if}</dl>
