<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { replaceState } from '$app/navigation';
  import { page } from '$app/state';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import AreaSeries from '@bagel/ui/svelte/AreaSeries.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { EnrollmentWire } from '#lib/server/services';
  import {
    DEFAULT_ENROLLMENT_WINDOW,
    ENROLLMENT_WINDOWS,
    parseEnrollmentWindow,
    type EnrollmentWindow
  } from '#lib/enrollment-window';

  type EnrollmentRead = Promise<{ value: EnrollmentWire; ok: boolean }>;
  type WindowRead = {
    source: EnrollmentRead;
    days: EnrollmentWindow;
    result: Promise<EnrollmentWire | null>;
  };

  let { enrollment, days }: { enrollment: EnrollmentRead; days: EnrollmentWindow } = $props();

  const { t, locale } = getI18n();
  const CHART_HEIGHT = 178;
  const WINDOW_LABEL = {
    7: 'admin.overview.window7',
    30: 'admin.overview.window30',
    90: 'admin.overview.window90'
  } as const;

  const options = ENROLLMENT_WINDOWS.map((window) => ({
    value: String(window),
    label: t(WINDOW_LABEL[window])
  }));

  let request = $state.raw<WindowRead | null>(null);
  const initial = $derived(enrollment.then(({ value, ok }) => ok ? value : null, () => null));
  const selected = $derived(request?.source === enrollment ? request : { days, result: initial });

  async function readWindow(next: EnrollmentWindow): Promise<EnrollmentWire | null> {
    try {
      const response = await fetch(`/overview/enrollment?days=${next}`);
      return response.ok ? await response.json() : null;
    } catch {
      return null;
    }
  }

  function selectWindow(raw: string) {
    const next = parseEnrollmentWindow(raw);
    request = { source: enrollment, days: next, result: readWindow(next) };

    const url = new URL(location.href);
    if (next === DEFAULT_ENROLLMENT_WINDOW) url.searchParams.delete('days');
    else url.searchParams.set('days', String(next));
    if (url.href !== location.href) void replaceState(url, page.state);
  }
</script>

{#snippet metrics(values: readonly number[] | undefined)}
  {@const today = values?.at(-1)}
  {@const week = today === undefined ? undefined : values?.slice(-7).reduce((sum, count) => sum + count, 0)}
  <div class="metrics">
    {#each [
      { label: t('admin.overview.statToday'), delta: t('admin.overview.statTodayDelta'), value: today },
      { label: t('admin.overview.statWeek'), delta: t('admin.overview.statWeekDelta'), value: week }
    ] as metric (metric.label)}
      {#if values === undefined}
        <Skeleton variant="block" height="100%" />
      {:else}
        <StatTile
          label={metric.label}
          value={metric.value?.toLocaleString(locale) ?? '–'}
          delta={metric.delta}
          fitCopy
          flat
          inline
          static
        />
      {/if}
    {/each}
  </div>
{/snippet}

<Card as="section">
  <CardHead title={t('admin.overview.enrollmentTitle')} headingLevel={2}>
    {#snippet actions()}
      <a class="bb-card-head__more" href="/users"><bb-fit>{t('admin.overview.allUsers')}</bb-fit></a>
    {/snippet}
  </CardHead>

  <SegmentedControl
    {options}
    label={t('admin.overview.enrollmentWindow')}
    bind:value={() => String(selected.days), selectWindow}
    even
  />

  <div class="enrollment-body" style:--chart-height={`${CHART_HEIGHT}px`}>
    {#await selected.result}
      <div class="chart" role="status" aria-label={t('common.loading')} aria-busy="true">
        <Skeleton variant="block" height="100%" />
      </div>
      {@render metrics(undefined)}
    {:then series}
      {@const values = series?.days.map((day) => day.count) ?? []}
      <div class="chart">
        {#if !series}
          <EmptyState title={t('admin.overview.enrollmentUnavailable')} role="status" />
        {:else if values.length < 2}
          <EmptyState title={t('admin.overview.enrollmentEmpty')} role="status" />
        {:else}
          <AreaSeries
            {values}
            height={CHART_HEIGHT}
            label={t('admin.overview.enrollmentChart', { days: String(selected.days) })}
          />
        {/if}
      </div>
      {@render metrics(values)}
    {/await}
  </div>
</Card>

<style>
  .enrollment-body {
    margin-block-start: var(--bb-space-4);
  }

  .chart {
    display: grid;
    align-items: center;
    block-size: var(--chart-height);
  }

  .metrics {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--bb-space-4);
    block-size: 76px;
    margin-block-start: var(--bb-space-4);
  }
</style>
