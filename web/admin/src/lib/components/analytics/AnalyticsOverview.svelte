<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import '@bagel/ui/styles/elements/collapse.css';
  import AnalyticsSection from './AnalyticsSection.svelte';
  import AnalyticsStat from './AnalyticsStat.svelte';
  import type { Kpi } from '#lib/analytics-page';

  let { metrics, degraded, empty }: { metrics?: Kpi[]; degraded?: boolean; empty?: boolean } = $props();
  const { t } = getI18n();
  const showStatus = $derived(degraded || empty);
  const placeholders = Array.from({ length: 8 }, (_, slot) => slot);
</script>

<AnalyticsSection id="overview">
  <div class="overview-body" aria-busy={!metrics}>
    <div class="bb-collapse" class:is-open={!showStatus} aria-hidden={showStatus} inert={showStatus}>
      <div>
        <div class="metrics">
          {#if metrics}
            {#each metrics as metric (metric.key)}<AnalyticsStat {metric} />{/each}
          {:else}
            {#each placeholders as slot (slot)}<AnalyticsStat />{/each}
          {/if}
        </div>
      </div>
    </div>
    <div class="status" aria-live="polite">
      {#if degraded}
        <AlertBanner>{t('admin.analytics.degraded')}</AlertBanner>
      {:else if empty}
        <EmptyState title={t('admin.analytics.empty')} />
      {/if}
    </div>
  </div>
</AnalyticsSection>

<style>
  .metrics {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: var(--bb-space-4);
  }
  @media (max-width: 1060px) {
    .metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
</style>
