<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-cluster` (../styles/elements/layout.css).
  // Astro twin: ../astro/Cluster.astro.
  //
  // WRAPPING IS THE DEFAULT, and `nowrap` is the opt-out. Every hand-written
  // row of chips or buttons in this tree that did not wrap was a bug waiting
  // for a long label or a translated string -- French runs 15-20% longer than
  // English and the console ships FR. A caller that genuinely needs one line
  // asks for it and owns the overflow.
  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  let {
    gap = 2,
    justify,
    align,
    nowrap = false,
    as: tag = 'div',
    class: className = '',
    children,
    ...rest
  }: {
    /** Step on the --bb-space ramp, not a length. */
    gap?: 1 | 2 | 3 | 4 | 5 | 6;
    justify?: 'start' | 'center' | 'end' | 'between';
    align?: 'baseline' | 'stretch';
    /** Opts out of wrapping. The caller then owns the overflow. */
    nowrap?: boolean;
    as?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-cluster',
      `bb-cluster--${gap}`,
      justify ? `bb-cluster--${justify}` : null,
      align ? `bb-cluster--${align}` : null,
      nowrap ? 'bb-cluster--nowrap' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
