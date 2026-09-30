<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/deck-layout.css';
  import type { Snippet } from 'svelte';

  type Own = {
    inspecting?: boolean;
    width?: string;
    class?: string;
    children?: Snippet;
  };

  let {
    inspecting = false,
    width,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const classes = $derived(
    ['bb-deck-layout', inspecting ? 'is-inspecting' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div class={classes} style={width ? `--deck-aside: ${width};` : undefined} {...rest}>{#if children}{@render children()}{/if}</div>
