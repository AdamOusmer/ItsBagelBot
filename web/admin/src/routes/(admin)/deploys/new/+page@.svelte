<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import ShipFlow from '#lib/components/deploys/ShipFlow.svelte';
  import type { PageProps } from './$types';

  let { data }: PageProps = $props();

  $effect(() => {
    document.documentElement.lang = data.locale;
  });
</script>

<ShipFlow initialPlan={data.planned} />
