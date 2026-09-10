<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte half of the CardHead parity fixture; its Astro twin is ./cardhead.astro.
  import CardHead from '../../svelte/CardHead.svelte';
  // `withAction` rather than `action`: the snippet below is named `action`.
  let { title = 'Recent', withAction = false }: { title?: string; withAction?: boolean } = $props();
</script>

{#if withAction}<CardHead {title}>{#snippet action()}<a class="bb-card-head__more" href="/x">All</a>{/snippet}</CardHead>{:else}<CardHead {title} />{/if}
