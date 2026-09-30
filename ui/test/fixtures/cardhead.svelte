<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import CardHead from '../../svelte/CardHead.svelte';
  let { title = 'Recent', withAction = false, headingLevel }: { title?: string; withAction?: boolean; headingLevel?: 1 | 2 | 3 | 4 | 5 | 6 } = $props();
</script>

{#if withAction}<CardHead {title} {headingLevel}>{#snippet actions()}<a class="bb-card-head__more" href="/x">All</a>{/snippet}</CardHead>{:else}<CardHead {title} {headingLevel} />{/if}
