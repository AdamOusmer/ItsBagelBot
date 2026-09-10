<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-stack` (../styles/elements/layout.css).
  // Astro twin: ../astro/Stack.astro.
  //
  // `gap` is a STEP on the spacing ramp (0-8), not a CSS length. A length prop
  // would let a caller write `gap="14px"`, and the gaps this element exists to
  // delete were 10, 14, 18 and 22px -- every one of them written by somebody
  // who had a length prop available.
  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  let {
    gap = 4,
    align,
    as: tag = 'div',
    class: className = '',
    children,
    ...rest
  }: {
    /** Step on the --bb-space ramp, not a length. */
    gap?: 0 | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8;
    align?: 'start' | 'center' | 'end';
    as?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-stack', `bb-stack--${gap}`, align ? `bb-stack--${align}` : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
