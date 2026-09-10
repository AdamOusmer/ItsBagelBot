<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import '../app.css';
  // Direct component import, not the @bagel/kit barrel: the boot path must
  // not statically reference every page's machinery, so per-route chunks stay
  // minimal (see shared/svelte-config.js for the long-task measurement behind
  // this rule).
  import RootShell from '@bagel/kit/components/RootShell.svelte';
  let { data, children } = $props();

  const DEFAULT_TITLE = 'ItsBagelBot Admin';
</script>

<svelte:head>
  <title>{DEFAULT_TITLE}</title>
</svelte:head>

<!-- `orbs` is RootShell's default; the ambient pair used to be two literal divs
     in app.html, which is why they never moved with the CSS that styles them. -->
<RootShell locale={data.locale}>{@render children()}</RootShell>
