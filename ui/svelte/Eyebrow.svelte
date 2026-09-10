<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-eyebrow` (../styles/elements/typography.css).
  // Astro twin: ../astro/Eyebrow.astro.
  //
  // A <span> and not an <h*>: an eyebrow is a label ABOUT the heading under
  // it, and giving it its own rank puts a second entry in the outline for one
  // idea. When the eyebrow genuinely IS the section's heading, the caller
  // reaches for `<Heading variant="eyebrow">` instead, which is why that
  // treatment exists on the heading contract as well as here.
  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    as: tag = 'span',
    class: className = '',
    children,
    ...rest
  }: {
    as?: 'span' | 'p' | 'div';
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-eyebrow', className || null].filter(Boolean).join(' '));
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
