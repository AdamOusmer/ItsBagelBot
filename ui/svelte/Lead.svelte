<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-lead` (../styles/elements/typography.css).
  // Astro twin: ../astro/Lead.astro.
  //
  // Its own element rather than `<Text size="lg" tone="muted">` because the
  // contract also carries a MEASURE (--bb-content-text). A lead that runs the
  // full container width under a centred hero is the single most common
  // reason a landing section reads as unedited, and a caller composing it out
  // of Text props would not think to add the max-width.
  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    as: tag = 'p',
    class: className = '',
    children,
    ...rest
  }: {
    as?: 'p' | 'div';
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-lead', className || null].filter(Boolean).join(' '));
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
