<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
</script>

<div class="page">
  <PageHead title="Recipes" />
  <EmptyState title="Nothing here yet" body="A placeholder for the recipes section." />
</div>

<style>
  .page {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
</style>
