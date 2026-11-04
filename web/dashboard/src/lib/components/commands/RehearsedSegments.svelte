<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Seg } from '@bagel/kit';

  let { segments, unknownTitle }: { segments: Seg[]; unknownTitle: string } = $props();
</script>

{#each segments as seg, i (i)}
  {#if seg.kind === 'sample'}<mark>{seg.text}</mark>
  {:else if seg.kind === 'unknown'}<mark class="bb-chat__unknown" title={unknownTitle}>{seg.text}</mark>
  {:else}{seg.text}{/if}
{/each}
