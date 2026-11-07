<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
</script>

<div class="page">
  <PageHead title="Overview" />
  <EmptyState title="Nothing here yet" body="A placeholder for the overview section." />
</div>

<style>
  .page {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
</style>
