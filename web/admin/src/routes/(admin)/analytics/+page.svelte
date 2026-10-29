<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { goto } from '$app/navigation';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import AreaSeries from '@bagel/ui/svelte/AreaSeries.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AnalyticsView, Table as ViewTable } from '$lib/analytics-page';

  let { data } = $props();
  const { t } = getI18n();

  let view = $state<AnalyticsView | null>(null);
  let degraded = $state(false);
  $effect(() => {
    let alive = true;
    data.bundle.then((bundle: { view: AnalyticsView; degraded: boolean }) => {
      if (!alive) return;
      view = bundle.view;
      degraded = bundle.degraded;
    });
    return () => {
      alive = false;
    };
  });

  const labels = $derived(data.windows.map((d: number) => t('admin.analytics.window', { days: String(d) })));
  const current = $derived(labels[data.windows.indexOf(data.days)] ?? labels[0]);

  function pick(label: string) {
    const days = data.windows[labels.indexOf(label)];
    if (days) goto(days === data.windows[0] ? '/analytics' : `/analytics?days=${days}`, { reset: false });
  }
</script>

{#snippet table(tbl: ViewTable)}
  <Card as="section">
    <CardHead title={t(`admin.analytics.table.${tbl.key}`)} />
    {#if tbl.rows.length}
      <Table label={t(`admin.analytics.table.${tbl.key}`)} compact>
        <thead>
          <tr>
            <th scope="col">{t('admin.analytics.column.label')}</th>
            {#each tbl.columns as col}<th scope="col">{t(`admin.analytics.column.${col}`)}</th>{/each}
          </tr>
        </thead>
        <tbody>
          {#each tbl.rows as row}
            <tr><th scope="row">{row.label}</th>{#each row.values as value}<td>{value}</td>{/each}</tr>
          {/each}
        </tbody>
      </Table>
    {:else}
      <EmptyState title={t('admin.analytics.noRows')} />
    {/if}
  </Card>
{/snippet}

<section class="screen active analytics">
  <PageHead eyebrow={t('admin.analytics.eyebrow')} title={t('admin.analytics.title')} description={t('admin.analytics.description')} />

  <div class="window">
    <SegmentedControl options={labels} label={t('admin.analytics.windowLabel')} bind:value={() => current, pick} />
  </div>

  {#if degraded}<AlertBanner>{t('admin.analytics.degraded')}</AlertBanner>{/if}

  {#if !view}
    <SkeletonStack rows={3} height="220px" />
  {:else}
    {#if view.empty}<EmptyState title={t('admin.analytics.empty')} />{/if}

    <div class="tiles">
      {#each view.tiles as tile}<StatTile label={t(`admin.analytics.tile.${tile.key}`)} value={tile.value} flat />{/each}
    </div>

    <div class="grid">
      {#each view.series as s}
        <Card as="section">
          <CardHead title={t(`admin.analytics.series.${s.key}`)} />
          {#if s.values.length > 1}
            <AreaSeries values={s.values} label={t(`admin.analytics.series.${s.key}`)} />
          {:else}
            <EmptyState title={t('admin.analytics.noRows')} />
          {/if}
        </Card>
      {/each}
    </div>

    <Card as="section">
      <CardHead title={t('admin.analytics.cohortTitle')} />
      <Text as="p" size="sm" tone="muted">{t('admin.analytics.cohortHint')}</Text>
      {#if view.cohorts.length}
        <Table label={t('admin.analytics.cohortTitle')} compact>
          <thead>
            <tr>
              <th scope="col">{t('admin.analytics.column.signupWeek')}</th>
              <th scope="col">{t('admin.analytics.column.signups')}</th>
              {#each view.cohorts[0].cells as _, i}<th scope="col">{t('admin.analytics.column.weekN', { n: String(i) })}</th>{/each}
            </tr>
          </thead>
          <tbody>
            {#each view.cohorts as line}
              <tr><th scope="row">{line.week}</th><td>{line.size}</td>{#each line.cells as cell}<td>{cell}</td>{/each}</tr>
            {/each}
          </tbody>
        </Table>
      {:else}
        <EmptyState title={t('admin.analytics.noRows')} />
      {/if}
    </Card>

    <div class="grid">
      {#each view.tables as tbl}{@render table(tbl)}{/each}
    </div>

    <Text as="p" size="sm" tone="muted">{t('admin.analytics.privacyNote')}</Text>
  {/if}
</section>

<style>
  .analytics {
    display: grid;
    gap: var(--bb-space-5);
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: var(--bb-space-4);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--bb-space-5);
  }
  @media (max-width: 900px) {
    .tiles {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .grid {
      grid-template-columns: 1fr;
    }
  }
</style>
