<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    htmlFor,
    class: className = '',
    children,
    ...rest
  }: {
    htmlFor?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-label', className || null].filter(Boolean).join(' '));
</script>

<label class={classes} for={htmlFor} {...rest}>{@render children()}</label>
