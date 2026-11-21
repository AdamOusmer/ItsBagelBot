<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import AnalyticsFacts from './AnalyticsFacts.svelte';
  import AnalyticsPanel from './AnalyticsPanel.svelte';
  import type { Fact } from '#lib/analytics-page';

  let { id, items, height = '8rem' }: { id: string; items?: Fact[]; height?: string } = $props();
</script>

<AnalyticsPanel {id} {height} loading={!items} empty={items?.length === 0}>
  {#if items}<AnalyticsFacts {items} />{/if}
</AnalyticsPanel>
