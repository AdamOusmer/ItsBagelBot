<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The working row an overview page is built around: one wide column carrying
  // the page's heaviest panel, and a rail of smaller reads beside it. Both
  // consoles land on this same shape, so the grid, its collapse point and the
  // margin rule below live here rather than in each page's own stylesheet.
  import type { Snippet } from 'svelte';

  let { main, side }: { main: Snippet; side: Snippet } = $props();
</script>

<div class="ov-row">
  <div class="ov-row__main">{@render main()}</div>
  <div class="ov-row__side">{@render side()}</div>
</div>

<style>
  /* The working row: the main panel carries the weight, the smaller reads stack
     beside it. Collapses to one column before the main panel's rows start
     truncating. */
  .ov-row {
    display: grid;
    grid-template-columns: 1.4fr 1fr;
    gap: var(--row-gap);
    align-items: start;
    margin-bottom: var(--row-gap);
  }
  .ov-row__main,
  .ov-row__side {
    min-width: 0;
  }
  .ov-row__side {
    display: flex;
    flex-direction: column;
    gap: var(--row-gap);
  }
  /* The panels in the rail each carry their own bottom margin from when the
     page was one vertical stack. Inside the rail that margin adds to the flex
     gap and every second slot ends up double-spaced (measured 28px then 56px),
     so the rail owns the spacing and the children contribute none. */
  .ov-row__side > :global(section) {
    margin-bottom: 0;
  }
  @media (max-width: 900px) {
    .ov-row {
      grid-template-columns: 1fr;
    }
  }
</style>
