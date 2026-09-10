<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-grid` (../styles/elements/layout.css).
  // Astro twin: ../astro/Grid.astro.
  //
  // `min` and `cols` are the two MODES, and passing `min` wins. Fixed columns
  // is "I know I want three"; auto-fit is "I know the smallest a cell may be".
  // They are not composable -- a grid cannot be both -- so the adapter picks
  // rather than emitting two conflicting template rules and letting source
  // order decide.
  //
  // `min` is the one length this file accepts, because it is a CONTENT
  // measurement (the narrowest a card may be before its label wraps), not a
  // rhythm value, and there is no ramp it could come from.
  import '../styles/elements/layout.css';
  import type { Snippet } from 'svelte';

  let {
    cols = 1,
    gap = 4,
    min,
    as: tag = 'div',
    class: className = '',
    children,
    ...rest
  }: {
    cols?: 1 | 2 | 3 | 4 | 5 | 6;
    /** Step on the --bb-space ramp, not a length. */
    gap?: 1 | 2 | 3 | 4 | 5 | 6;
    /** Auto-fit mode: the narrowest a cell may be. Overrides `cols`. */
    min?: string;
    as?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-grid',
      min ? 'bb-grid--auto' : `bb-grid--${cols}`,
      `bb-grid--gap-${gap}`,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element
  this={tag}
  class={classes}
  style={min ? `--grid-min: ${min};` : undefined}
  {...rest}>{@render children()}</svelte:element
>
