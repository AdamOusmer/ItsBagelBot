<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import { plottable, type LineSeriesSpec } from '@bagel/ui/lib/line-series';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Disclosure from '@bagel/ui/svelte/Disclosure.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import LineSeries from '@bagel/ui/svelte/LineSeries.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import { rateLabel } from './shard-state';
  import type { ThroughputPoint } from './throughput-history';

  let { points, showTrials }: { points: readonly ThroughputPoint[]; showTrials: boolean } = $props();
  const { locale, t } = getI18n();
  let readingsOpen = $state(false);
  const readings = $derived(readingsOpen ? points.filter((point) => Number.isFinite(point.at)).reverse() : []);
  const series = $derived<LineSeriesSpec[]>([
    { key: 'production', label: t('admin.shards.throughputProduction') },
    ...(showTrials ? [{ key: 'trials', label: t('admin.shards.throughputTrials'), tone: 'warm' as const, dashed: true }] : [])
  ]);
  const time = new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' });

  function formatRate(value: number | null | undefined): string {
    const rate = plottable(value);
    return rate === null ? t('admin.shards.throughputUnavailable') : `${rateLabel(rate)} ${t('admin.shards.eps')}`;
  }
</script>

<Card as="section" aria-labelledby="throughput-title">
  <div class="chart-frame">
    <header class="chart-head">
      <Heading level={5} as="h2" id="throughput-title"><bb-fit><span data-fit>{t('admin.shards.throughputHistory')}</span></bb-fit></Heading>
      <div class="chart-window">
        <Text size="xs" mono tone="muted"><bb-fit><span data-fit>{t('admin.shards.throughputWindow')}</span></bb-fit></Text>
      </div>
    </header>
    <div class="chart-hint">
      <Text size="sm" tone="muted" style="block-size: 100%">
        <bb-fit mode="block"><span data-fit>{t('admin.shards.throughputHistoryHint')}</span></bb-fit>
      </Text>
    </div>

    <div class="plot-slot">
      <LineSeries
        {points}
        {series}
        label={t('admin.shards.throughputHistory')}
        description={t('admin.shards.throughputHistoryHint')}
        unit={t('admin.shards.eps')}
        emptyLabel={t('admin.shards.throughputSampling')}
        formatValue={formatRate}
        formatTime={time.format}
      />
    </div>

    <Disclosure size="sm" summary={t('admin.shards.throughputData')} bind:open={readingsOpen}>
      {#if readings.length}
        <Table label={t('admin.shards.throughputHistory')}>
          <caption><VisuallyHidden>{t('admin.shards.throughputHistory')}</VisuallyHidden></caption>
          <thead>
            <tr>
              <th scope="col">{t('admin.shards.throughputTime')}</th>
              <th scope="col">{t('admin.shards.throughputProduction')}</th>
              {#if showTrials}<th scope="col">{t('admin.shards.throughputTrials')}</th>{/if}
            </tr>
          </thead>
          <tbody>
            {#each readings as point (point.at)}
              <tr>
                <th scope="row">{time.format(point.at)}</th>
                <td>{formatRate(point.production)}</td>
                {#if showTrials}<td>{formatRate(point.trials)}</td>{/if}
              </tr>
            {/each}
          </tbody>
        </Table>
      {:else if readingsOpen}
        <EmptyState title={t('admin.shards.throughputSampling')} />
      {/if}
    </Disclosure>
  </div>
</Card>

<style>
  .chart-frame {
    container-type: inline-size;
    min-inline-size: 0;
  }

  .chart-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr) min(38%, 10rem);
    gap: var(--bb-space-3);
    align-items: baseline;
  }

  .chart-window {
    text-align: end;
  }

  .chart-hint {
    block-size: 1.5rem;
    margin-block-start: var(--bb-space-2);
  }

  .plot-slot {
    min-block-size: 21.5rem;
  }

  @container (max-width: 650px) {
    .chart-hint {
      block-size: 2.7rem;
    }
  }

  @container (max-width: 499px) {
    .plot-slot {
      min-block-size: 19.25rem;
    }
  }

  @container (max-width: 400px) {
    .chart-head {
      grid-template-columns: minmax(0, 1fr);
      gap: var(--bb-space-1);
    }

    .chart-window {
      text-align: start;
    }
  }

  @container (max-width: 340px) {
    .chart-hint {
      block-size: 3.75rem;
    }
  }
</style>
