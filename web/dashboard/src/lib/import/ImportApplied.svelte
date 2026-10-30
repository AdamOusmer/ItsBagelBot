<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';

  let { tiles, staggered = false, inline = false, style }: {
    tiles: readonly { label: string; n: number }[];
    staggered?: boolean;
    inline?: boolean;
    style?: string;
  } = $props();
</script>

{#if tiles.length}
  <div class="applied bb-stat-grid bb-stat-grid--auto" class:bb-stagger={staggered} {style}>
    {#each tiles as a (a.label)}
      <StatTile static {inline} label={a.label} value={String(a.n)} />
    {/each}
  </div>
{/if}

<style>
  .applied { margin: 22px 0 26px; }

  @media (max-width: 760px), (max-height: 760px) {
    .applied { margin: 12px 0 14px; }
  }
</style>
