<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/fact-list.css';
  import type { Snippet } from 'svelte';

  type Own = {
    layout?: 'rows' | 'tiles' | 'inline';
    class?: string;
    children?: Snippet;
  };

  let {
    layout = 'rows',
    class: className = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements['dl'], keyof Own> = $props();

  const classes = $derived(
    ['bb-facts', layout === 'rows' ? null : `bb-facts--${layout}`, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<dl class={classes} {...rest}>{#if children}{@render children()}{/if}</dl>
