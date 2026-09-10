<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // A run of identical loading blocks, stacked or in a grid. Every streamed
  // section on an overview page renders one of these while its read is in
  // flight, and each had its own three-line flex/grid wrapper beside its own
  // `{#each}`; the shapes agreed by accident, and the aria-hidden was dropped
  // from one of them.
  //
  // aria-hidden lives on the wrapper here rather than at the call site: the
  // placeholders carry no information, and the section that owns them already
  // announces the wait with aria-busy plus a visually hidden label. Left
  // exposed, a screen reader walks a run of empty blocks for every panel.
  import Skeleton from './Skeleton.svelte';

  let {
    rows,
    height,
    columns = 1
  }: {
    /** How many placeholder blocks to draw. */
    rows: number;
    /** CSS height of one block, e.g. '260px'. */
    height: string;
    /** 1 stacks them; 2 lays them out two-up (one-up on a phone). */
    columns?: 1 | 2;
  } = $props();
</script>

<div class={columns === 2 ? 'bb-skel-stack bb-skel-stack--grid' : 'bb-skel-stack'} aria-hidden="true">
  {#each Array(rows) as _, i (i)}<Skeleton variant="block" {height} />{/each}
</div>
