<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-ov-row` contract
  // (../styles/elements/overview-grid.css). Astro twin: ../astro/OverviewGrid.astro.
  //
  // The working row both overview pages are built around: one wide column
  // carrying the page's heaviest panel, and a rail of smaller reads beside it.
  // Both consoles land on this shape, so the grid, its collapse point and the
  // margin rule live in the contract rather than in each page's stylesheet.
  import '../styles/elements/overview-grid.css';
  import type { Snippet } from 'svelte';

  let {
    class: className = '',
    main,
    side,
    ...rest
  }: {
    class?: string;
    /** The wide column. */
    main?: Snippet;
    /** The rail of smaller panels beside it. */
    side?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-ov-row', className || null].filter(Boolean).join(' '));
</script>

<div class={classes} {...rest}><div class="bb-ov-row__main">{#if main}{@render main()}{/if}</div><div
    class="bb-ov-row__side"
  >{#if side}{@render side()}{/if}</div></div>
