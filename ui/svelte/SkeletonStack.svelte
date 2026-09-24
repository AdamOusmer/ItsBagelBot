<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import Skeleton from './Skeleton.svelte';

  let {
    rows,
    height,
    columns = 1
  }: {
    rows: number;
    height: string;
    columns?: 1 | 2;
  } = $props();
</script>

<div class={columns === 2 ? 'bb-skel-stack bb-skel-stack--grid' : 'bb-skel-stack'} aria-hidden="true">
  {#each Array(rows) as _, i (i)}<Skeleton variant="block" {height} />{/each}
</div>
