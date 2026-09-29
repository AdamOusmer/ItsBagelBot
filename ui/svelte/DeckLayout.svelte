<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/deck-layout.css';
  import type { Snippet } from 'svelte';

  let {
    inspecting = false,
    width,
    class: className = '',
    children,
    ...rest
  }: {
    inspecting?: boolean;
    width?: string;
    class?: string;
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-deck-layout', inspecting ? 'is-inspecting' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div class={classes} style={width ? `--deck-aside: ${width};` : undefined} {...rest}>{#if children}{@render children()}{/if}</div>
