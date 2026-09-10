<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-sr-only` (../styles/elements/typography.css).
  // Astro twin: ../astro/VisuallyHidden.astro.
  //
  // The rule it applies had been written out by hand inside two other
  // contracts (elements/nav-link.css and elements/shell.css), both with a
  // comment saying the package did not ship one. It does now, and both of
  // those point here.
  //
  // `focusable` is the skip-link case: hidden until it takes focus, then a
  // real control. Anything else that becomes visible on focus is a tooltip and
  // should be one.
  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    focusable = false,
    as: tag = 'span',
    class: className = '',
    children,
    ...rest
  }: {
    /** Reveals itself when focused, for skip links. */
    focusable?: boolean;
    as?: 'span' | 'div' | 'p' | 'a';
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-sr-only', focusable ? 'bb-sr-only--focusable' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
