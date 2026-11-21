<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Disclosure from '@bagel/ui/svelte/Disclosure.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import LineSeries from '@bagel/ui/svelte/LineSeries.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { LineSeriesSpec } from '@bagel/ui/lib/line-series';
  import AnalyticsFacts from './AnalyticsFacts.svelte';
  import AnalyticsPanel from './AnalyticsPanel.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { Flow, FlowPoint, GainLoss } from '#lib/analytics-page';

  let { id, flow, gainLoss }: { id: string; flow?: Flow; gainLoss?: GainLoss } = $props();
  const { locale, t } = getI18n();
  const number = new Intl.NumberFormat(locale);
  const signed = new Intl.NumberFormat(locale, { signDisplay: 'exceptZero' }).format;
  const formatDay = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format;
  const formatValue = (value: number | null | undefined) => value == null ? '' : number.format(value);
  const title = $derived(t(`admin.analytics.card.${id}`));
  const isGainLoss = $derived(id === 'gainLoss');
  const points = $derived<FlowPoint[]>(flow?.points ?? gainLoss?.points ?? []);
  const series = $derived<LineSeriesSpec[]>(flow?.lines.map((line) => ({
    key: line.key, label: t(line.name.key, line.name.params), tone: line.tone, dashed: line.dashed
  })) ?? [
    { key: 'gained', label: t('admin.analytics.chart.gained'), tone: 'accent' },
    { key: 'lost', label: t('admin.analytics.chart.lost'), tone: 'danger' }
  ]);
  let showData = $state(false);
</script>

<AnalyticsPanel {id} height="25rem" loading={!flow && !gainLoss} empty={points.length === 0} footerHeight={isGainLoss ? undefined : '8.5rem'}>
  {#snippet summary()}
    {#if isGainLoss}
      <div class="flow-summary">
        {#if gainLoss}
          <Text as="p" size="xs" tone="muted">
            <bb-fit><span data-fit>{t('admin.analytics.chart.gainLossSummary', {
              gained: number.format(gainLoss.gained), lost: number.format(gainLoss.lost), net: signed(gainLoss.net)
            })}</span></bb-fit>
          </Text>
        {/if}
      </div>
    {/if}
  {/snippet}
  {#snippet footer()}
    {#if flow}
      <AnalyticsFacts items={flow.facts} />
    {:else}
      <div aria-hidden="true"><Skeleton variant="block" height="8.5rem" /></div>
    {/if}
  {/snippet}
  {#if points.length === 1}
    <FactList layout="tiles">
      {#each series as line (line.key)}
        <Fact term={line.label} fit>{formatValue(points[0][line.key])}</Fact>
      {/each}
    </FactList>
  {:else}
    <LineSeries {points} {series} label={title} {formatValue} formatTime={formatDay} gapMs={86_400_000} />
    {#if gainLoss}
      <Text as="p" size="xs" tone="muted">{t('admin.analytics.chart.lossDays', { days: number.format(gainLoss.lossDays) })}</Text>
    {/if}
    <Disclosure summary={t('admin.analytics.chart.showData')} size="sm" bind:open={showData}>
      {#if showData}
        <div class="data">
          <Table label={title} compact>
            <thead>
              <tr>
                <th scope="col">{t('admin.analytics.chart.date')}</th>
                {#each series as line (line.key)}<th scope="col" class="r">{line.label}</th>{/each}
              </tr>
            </thead>
            <tbody>
              {#each points as point (point.at)}
                <tr>
                  <th scope="row">{formatDay(point.at)}</th>
                  {#each series as line (line.key)}<td class="r">{formatValue(point[line.key])}</td>{/each}
                </tr>
              {/each}
            </tbody>
          </Table>
        </div>
      {/if}
    </Disclosure>
  {/if}
</AnalyticsPanel>

<style>
  .flow-summary { min-block-size: 1.4rem; }
  .data {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: minmax(0, 1fr);
    block-size: 14rem;
    min-inline-size: 0;
  }
</style>
