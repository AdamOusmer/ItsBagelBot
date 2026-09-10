<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-section` (../styles/elements/layout.css).
  // Astro twin: ../astro/Section.astro.
  //
  // `reveal` emits the shared scroll-reveal attribute (../styles/reveal.css)
  // rather than taking an animation prop: the reveal is one behaviour with one
  // engine (ui/lib/reveal.ts) and one reduced-motion story, and a section that
  // wanted its own would be re-implementing the part that was hard.
  //
  // `anchor` (position: relative) is opt-in and not the default. A section
  // that is relative becomes the containing block for everything absolutely
  // positioned inside it, which is correct for a band with its own ornament
  // and wrong for one whose child meant to position against the page -- the
  // light field and the orb layers both do.
  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  let {
    size = 'default',
    anchor = false,
    reveal = false,
    as: tag = 'section',
    class: className = '',
    children,
    ...rest
  }: {
    size?: 'default' | 'sm' | 'lg' | 'flush';
    /** position: relative, for a band that hosts its own ornament. */
    anchor?: boolean;
    /** Opts into the shared scroll-reveal contract. */
    reveal?: boolean;
    as?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-section',
      size === 'default' ? null : `bb-section--${size}`,
      anchor ? 'bb-section--anchor' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} data-reveal={reveal ? '' : undefined} {...rest}
  >{@render children()}</svelte:element
>
