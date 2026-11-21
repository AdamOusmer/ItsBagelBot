<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import '../app.css';
  import RootShell from '@bagel/kit/components/RootShell.svelte';
  let { data, children } = $props();

  const DEFAULT_TITLE = 'ItsBagelBot Admin';
</script>

<svelte:head>
  <title>{DEFAULT_TITLE}</title>
</svelte:head>

{#key data.locale}
  <RootShell locale={data.locale} cursorEnabled={data.cursorEnabled}>
    {@render children()}
  </RootShell>
{/key}
