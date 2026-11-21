<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import RunDetail from '#lib/components/deploys/RunDetail.svelte';
  import type { PageProps } from './$types';

  let { data }: PageProps = $props();
</script>

{#key data.run.id}
  <RunDetail initial={data.run} />
{/key}
