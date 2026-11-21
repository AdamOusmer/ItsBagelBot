<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { Kpi } from '#lib/analytics-page';

  let { metric, label }: { metric?: Kpi; label?: string } = $props();
  const { t } = getI18n();
  const delta = $derived(metric ? t(metric.delta.key, metric.delta.params) : '');
  const unit = $derived(metric?.unit ? t(metric.unit.key, metric.unit.params) : '');
</script>

<div class="metric">
  {#if metric}
    <StatTile
      label={label ?? t(`admin.analytics.kpi.${metric.key}`)}
      value={metric.value}
      {unit}
      {delta}
      flat={metric.trend !== 'up'}
      fitCopy
      static
    />
  {:else}
    <div aria-hidden="true"><Skeleton variant="block" height="100%" /></div>
  {/if}
</div>

<style>
  .metric {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    min-inline-size: 0;
    min-block-size: 10rem;
    overflow-wrap: anywhere;
  }
</style>
