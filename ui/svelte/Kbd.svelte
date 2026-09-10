<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-kbd` (../styles/elements/typography.css).
  // Astro twin: ../astro/Kbd.astro.
  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    class: className = '',
    children,
    ...rest
  }: { class?: string; children: Snippet; [key: string]: unknown } = $props();

  const classes = $derived(['bb-kbd', className || null].filter(Boolean).join(' '));
</script>

<kbd class={classes} {...rest}>{@render children()}</kbd>
