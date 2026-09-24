<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import Card from '../../svelte/Card.svelte';
  let { banded = false, ...props }: { banded?: boolean; [key: string]: unknown } = $props();
</script>

{#if banded}<Card {...props}>{#snippet band()}<i>B</i>{/snippet}body</Card>{:else}<Card {...props}>body</Card>{/if}
