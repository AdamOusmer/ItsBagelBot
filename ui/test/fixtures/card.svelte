<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte half of the Card parity fixture; its Astro twin is ./card.astro.
  import Card from '../../svelte/Card.svelte';
  // `banded` rather than `band`: the snippet below is named `band`, and a prop
  // of the same name would shadow it.
  let { banded = false, ...props }: { banded?: boolean; [key: string]: unknown } = $props();
</script>

{#if banded}<Card {...props}>{#snippet band()}<i>B</i>{/snippet}body</Card>{:else}<Card {...props}>body</Card>{/if}
