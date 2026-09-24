<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import type { Snippet } from 'svelte';
  let { exitHref, exitForm = false, exitLabel = 'Exit', children }:
    { exitHref?: string; exitForm?: boolean; exitLabel?: string; children: Snippet } = $props();
</script>

<AlertBanner variant="impersonation" role="status">
  {@render children()}
  {#snippet action()}
    {#if exitForm}
      <form method="POST" action="/auth/logout"><button type="submit" class="bb-alert__exit">{exitLabel}</button></form>
    {:else}
      <a href={exitHref} class="bb-alert__exit">{exitLabel}</a>
    {/if}
  {/snippet}
</AlertBanner>
