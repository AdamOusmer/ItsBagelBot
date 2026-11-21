<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AreaSeries from '@bagel/ui/svelte/AreaSeries.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import AnalyticsPanel from './AnalyticsPanel.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { Series } from '#lib/analytics-page';

  let { id, series }: { id: string; series?: Series } = $props();
  const { locale, t } = getI18n();
  const title = $derived(t(`admin.analytics.card.${id}`));
  const formatCount = new Intl.NumberFormat(locale).format;
  const formatDate = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format;
  const labels = $derived(series?.labels.map((day) => formatDate(new Date(day))) ?? []);
</script>

<AnalyticsPanel {id} height="200px" loading={!series} empty={series?.values.length === 0}>
  {#snippet summary()}
    <div class="chart-summary">
      <Text as="p" size="xs" tone="muted">
        {#if series?.values.length}
          <bb-fit><span data-fit>{t('admin.analytics.chart.summary', { average: series.average, peak: series.peak })}</span></bb-fit>
        {/if}
      </Text>
    </div>
  {/snippet}
  {#if series}
    {#if series.values.length === 1}
      <StatTile label={labels[0]} value={formatCount(series.values[0])} fitCopy static />
    {:else}
      <AreaSeries values={series.values} {labels} ticks={series.ticks} label={title} uid={id} formatValue={formatCount} />
      <div class="axis">
        <Text as="span" size="xs" tone="muted">{labels[0]}</Text>
        <Text as="span" size="xs" tone="muted">{labels.at(-1)}</Text>
      </div>
    {/if}
  {/if}
</AnalyticsPanel>

<style>
  .chart-summary {
    min-block-size: 1.4em;
  }
  .axis {
    display: flex;
    justify-content: space-between;
    gap: var(--bb-space-3);
  }
</style>
