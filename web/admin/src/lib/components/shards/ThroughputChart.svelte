<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import { clockTime, type LineSeriesSpec } from '@bagel/ui/lib/line-series';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Disclosure from '@bagel/ui/svelte/Disclosure.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import LineSeries from '@bagel/ui/svelte/LineSeries.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import { rateLabel } from './shard-state';

  type Point = { at: number; production: number | null; trials: number | null };
  let { points, showTrials }: { points: Point[]; showTrials: boolean } = $props();
  const { t } = getI18n();
  const readings = $derived(points.filter((point) => Number.isFinite(point.at)).toSorted((a, b) => a.at - b.at));
  const series = $derived<LineSeriesSpec[]>([
    { key: 'production', label: t('admin.shards.throughputProduction') },
    ...(showTrials ? [{ key: 'trials', label: t('admin.shards.throughputTrials'), tone: 'tan' as const, dashed: true }] : [])
  ]);

  function finiteRate(value: number | null | undefined): number | null { return value != null && Number.isFinite(value) && value >= 0 ? value : null; }
  function label(value: number | null | undefined): string {
    const rate = finiteRate(value);
    return rate === null ? t('admin.shards.throughputUnavailable') : `${rateLabel(rate)} ${t('admin.shards.eps')}`;
  }
</script>

<div class="chart">
  <Card as="section" aria-labelledby="throughput-title">
    <header class="chart-head">
      <div class="chart-text"><Heading level={5} as="h2" id="throughput-title">{t('admin.shards.throughputHistory')}</Heading><Text size="xs" tone="muted">{t('admin.shards.throughputHistoryHint')}</Text></div>
      <Text as="span" size="xs" mono tone="muted">{t('admin.shards.throughputWindow')}</Text>
    </header>
    <LineSeries
      {points}
      {series}
      ariaLabel={t('admin.shards.throughputHistory')}
      description={t('admin.shards.throughputHistoryHint')}
      unit={t('admin.shards.eps')}
      emptyLabel={t('admin.shards.throughputSampling')}
      formatValue={label}
    />
    {#if readings.length}
      <div class="readings">
        <Disclosure size="sm" summary={t('admin.shards.throughputData')}>
          <Scroller maxHeight="220px">
            <Table label={t('admin.shards.throughputHistory')} compact>
              <caption><VisuallyHidden>{t('admin.shards.throughputHistory')}</VisuallyHidden></caption>
              <thead><tr><th scope="col">{t('admin.shards.throughputTime')}</th><th scope="col">{t('admin.shards.throughputProduction')}</th>{#if showTrials}<th scope="col">{t('admin.shards.throughputTrials')}</th>{/if}</tr></thead>
              <tbody>{#each readings.toReversed() as point}<tr><th scope="row">{clockTime(point.at)}</th><td>{label(point.production)}</td>{#if showTrials}<td>{label(point.trials)}</td>{/if}</tr>{/each}</tbody>
            </Table>
          </Scroller>
        </Disclosure>
      </div>
    {/if}
  </Card>
</div>

<style>
  .chart { margin-top: var(--bb-space-5); }
  .chart-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--bb-space-4); }
  .chart-text { display: grid; gap: var(--bb-space-1); }
  .readings { margin-top: var(--bb-space-2); }
  @media (max-width: 600px) { .chart { --card-pad: var(--bb-space-4); } .chart-head { flex-wrap: wrap; gap: var(--bb-space-2); } }
</style>
