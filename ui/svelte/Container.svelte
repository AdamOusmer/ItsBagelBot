<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-container` (../styles/elements/layout.css).
  // Astro twin: ../astro/Container.astro.
  //
  // Three widths and no free `max-width` prop. A caller that can pass any
  // number passes the number that looked right in the component it was
  // writing, which is how this tree ended up with 1100, 1120, 1180 and 1200
  // all meaning "the content column". The three named widths are the three
  // that mean something: the page column, the editor column, and a reading
  // measure. Anything else is a page-specific layout and belongs to the page.
  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  let {
    width = 'default',
    flush = false,
    as: tag = 'div',
    class: className = '',
    children,
    ...rest
  }: {
    width?: 'default' | 'narrow' | 'text';
    /** Drops the viewport gutter, for a container nested inside one. */
    flush?: boolean;
    as?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-container',
      width === 'default' ? null : `bb-container--${width}`,
      flush ? 'bb-container--flush' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
