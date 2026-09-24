<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import ShipFlow from '$lib/components/deploys/ShipFlow.svelte';

  let { data } = $props();
</script>

<ShipFlow plan={data.plan} planError={data.planError} />
