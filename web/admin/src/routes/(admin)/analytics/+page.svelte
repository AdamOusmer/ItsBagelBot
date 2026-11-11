<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { goto } from '$app/navigation';
  import type { ComponentProps, Snippet } from 'svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import SectionNav from '@bagel/ui/svelte/SectionNav.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import AreaSeries from '@bagel/ui/svelte/AreaSeries.svelte';
  import LineSeries from '@bagel/ui/svelte/LineSeries.svelte';
  import type { LineSeriesSpec } from '@bagel/ui/lib/line-series';
  import RankingCard, { type RankingItem } from '@bagel/ui/svelte/RankingCard.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AnalyticsView, Bar, DataTable, Fact as ViewFact, GainLoss, Name, Phrase, Series } from '#lib/analytics-page';
  import { dateLabel, signed } from '#lib/analytics-view';

  let { data } = $props();
  const { t } = getI18n();

  const SECTIONS = ['overview', 'growth', 'engagement', 'retention', 'revenue', 'features', 'commands', 'audience'] as const;
  const FIRST_COLUMN: Record<string, string> = {
    plans: 'plan',
    conversion: 'signupWeek',
    modules: 'module',
    builtIn: 'command',
    breadth: 'modulesOn',
  };
  const DAY_MS = 86_400_000;
  const SKELETON_HEIGHT = { series: '178px', lines: '330px', bars: '220px', table: '240px', facts: '120px', cohort: '320px' } as const;
  type TagTone = ComponentProps<typeof Tag>['tone'];
  const number = new Intl.NumberFormat('en-US');
  const formatCount = (value: number) => number.format(value);
  const formatDay = (at: number) => dateLabel(new Date(at).toISOString().slice(0, 10));

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

  const labels = $derived(data.windows.map((d: number) =>
    d === 1 ? t('admin.analytics.daily') : t('admin.analytics.window', { days: String(d) })));
  const current = $derived(labels[data.windows.indexOf(data.days)] ?? labels[0]);
  const navItems = $derived(SECTIONS.map((id) => ({ href: `#${id}`, label: t(`admin.analytics.nav.${id}`) })));
  const gainLossSeries = $derived<LineSeriesSpec[]>([
    { key: 'gained', label: t('admin.analytics.chart.gained'), tone: 'accent' },
    { key: 'lost', label: t('admin.analytics.chart.lost'), tone: 'danger' }
  ]);
  const weekColumns = $derived(Array.from({ length: view?.retention.horizon ?? 0 }, (_, week) => week));

  function pick(label: string) {
    const days = data.windows[labels.indexOf(label)];
    if (days) goto(days === data.defaultDays ? '/analytics' : `/analytics?days=${days}`, { reset: false });
  }

  const phrase = (p: Phrase) => t(p.key, p.params);
  const nameText = (name: Name) => ('text' in name ? name.text : t(name.key, name.params));
  const rankingItems = (bars: Bar[]): RankingItem[] =>
    bars.map((bar) => ({
      id: bar.id,
      label: nameText(bar.name),
      value: bar.value,
      valueLabel: bar.valueText,
      secondary: bar.secondary ? phrase(bar.secondary) : undefined,
    }));
</script>

{#snippet head(id: string, actions?: Snippet)}
  <CardHead title={t(`admin.analytics.card.${id}`)} {actions} />
  <Text as="p" size="sm" tone="muted">{t(`admin.analytics.hint.${id}`)}</Text>
{/snippet}

{#snippet loadingCard(id: string, height: string)}
  <Card as="section">
    {@render head(id)}
    <Skeleton variant="block" {height} />
  </Card>
{/snippet}

{#snippet seriesCard(id: string, series: Series | undefined)}
  {#if !series}
    {@render loadingCard(id, SKELETON_HEIGHT.series)}
  {:else}
    {#snippet summary()}
      <Text as="span" size="xs" tone="muted">
        {t('admin.analytics.chart.summary', { average: series.average, peak: series.peak })}
      </Text>
    {/snippet}
    <Card as="section">
      {@render head(id, summary)}
      {#if series.values.length > 1}
        <AreaSeries
          values={series.values}
          labels={series.labels}
          ticks={series.ticks}
          label={t(`admin.analytics.card.${id}`)}
          uid={id}
          formatValue={formatCount}
        />
        <div class="axis">
          <Text as="span" size="xs" tone="muted">{series.labels[0]}</Text>
          <Text as="span" size="xs" tone="muted">{series.labels[series.labels.length - 1]}</Text>
        </div>
      {:else if series.values.length === 1}
        <div class="chart-box"><StatTile label={series.labels[0]} value={formatCount(series.values[0])} static /></div>
      {:else}
        <div class="chart-box"><EmptyState title={t('admin.analytics.noRows')} /></div>
      {/if}
    </Card>
  {/if}
{/snippet}

{#snippet gainLossCard(flow: GainLoss | undefined)}
  {#if !flow}
    {@render loadingCard('gainLoss', SKELETON_HEIGHT.lines)}
  {:else}
    {#snippet summary()}
      <Text as="span" size="xs" tone="muted">
        {t('admin.analytics.chart.gainLossSummary', {
          gained: formatCount(flow.gained),
          lost: formatCount(flow.lost),
          net: signed(flow.net)
        })}
      </Text>
    {/snippet}
    <Card as="section">
      {@render head('gainLoss', summary)}
      {#if flow.points.length > 1}
        <LineSeries
          points={flow.points}
          series={gainLossSeries}
          label={t('admin.analytics.card.gainLoss')}
          formatValue={(value) => (value == null ? '' : formatCount(value))}
          formatTime={formatDay}
          gapMs={DAY_MS}
        />
        <Text as="span" size="xs" tone="muted">{t('admin.analytics.chart.lossDays', { days: formatCount(flow.lossDays) })}</Text>
      {:else if flow.points.length === 1}
        <div class="chart-box">
          <FactList layout="tiles">
            {#each gainLossSeries as line (line.key)}
              <Fact term={line.label}>{formatCount(flow.points[0][line.key as 'gained' | 'lost'])}</Fact>
            {/each}
          </FactList>
        </div>
      {:else}
        <div class="chart-box"><EmptyState title={t('admin.analytics.noRows')} /></div>
      {/if}
    </Card>
  {/if}
{/snippet}

{#snippet barCard(id: string, bars: Bar[] | undefined, facts?: ViewFact[])}
  {#snippet footer()}
    <FactList layout="tiles">
      {#each facts ?? [] as fact (fact.key)}
        <Fact term={t(`admin.analytics.fact.${fact.key}`)}>{fact.value}</Fact>
      {/each}
    </FactList>
  {/snippet}
  {#if !bars}
    {@render loadingCard(id, SKELETON_HEIGHT.bars)}
  {:else}
    <RankingCard
      title={t(`admin.analytics.card.${id}`)}
      description={t(`admin.analytics.hint.${id}`)}
      items={rankingItems(bars)}
      compact
      emptyLabel={t('admin.analytics.noRows')}
      footer={facts ? footer : undefined}
    />
  {/if}
{/snippet}

{#snippet factsCard(id: string, facts: ViewFact[] | undefined)}
  {#if !facts}
    {@render loadingCard(id, SKELETON_HEIGHT.facts)}
  {:else}
    <Card as="section">
      {@render head(id)}
      <FactList layout="tiles">
        {#each facts as fact (fact.key)}
          <Fact term={t(`admin.analytics.fact.${fact.key}`)}>{fact.value}</Fact>
        {/each}
      </FactList>
    </Card>
  {/if}
{/snippet}

{#snippet tableCard(id: string, tbl: DataTable | undefined, minWidth?: string, facts?: ViewFact[])}
  {#if !tbl}
    {@render loadingCard(id, SKELETON_HEIGHT.table)}
  {:else}
    <Card as="section">
      {@render head(id)}
      {#if tbl.rows.length}
        <Table label={t(`admin.analytics.card.${id}`)} compact {minWidth}>
          <thead>
            <tr>
              <th scope="col">{t(`admin.analytics.column.${FIRST_COLUMN[id]}`)}</th>
              {#each tbl.columns as col (col)}<th scope="col" class="r">{t(`admin.analytics.column.${col}`)}</th>{/each}
            </tr>
          </thead>
          <tbody>
            {#each tbl.rows as row (row.id)}
              <tr>
                <th scope="row">
                  {#if id === 'plans'}<Tag tone={row.id as TagTone}>{nameText(row.name)}</Tag>{:else}{nameText(row.name)}{/if}
                </th>
                {#each row.cells as cell}<td class="r">{cell.text}</td>{/each}
              </tr>
            {/each}
          </tbody>
        </Table>
        {#if facts}
          <FactList layout="tiles">
            {#each facts as fact (fact.key)}
              <Fact term={t(`admin.analytics.fact.${fact.key}`)}>{fact.value}</Fact>
            {/each}
          </FactList>
        {/if}
      {:else}
        <div class="chart-box"><EmptyState title={t('admin.analytics.noRows')} /></div>
      {/if}
    </Card>
  {/if}
{/snippet}

{#snippet cohortCard()}
  {#if !view}
    {@render loadingCard('cohorts', SKELETON_HEIGHT.cohort)}
  {:else}
    <Card as="section">
      {@render head('cohorts')}
      {#if view.retention.rows.length}
        <Table label={t('admin.analytics.card.cohorts')} compact minWidth="880px">
          <thead>
            <tr>
              <th scope="col">{t('admin.analytics.column.signupWeek')}</th>
              <th scope="col" class="r">{t('admin.analytics.column.signups')}</th>
              {#each weekColumns as week (week)}
                <th scope="col" class="r">{t('admin.analytics.column.weekN', { n: String(week) })}</th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each view.retention.rows as line (line.week)}
              <tr>
                <th scope="row">{line.week}</th>
                <td class="r">{line.size}</td>
                {#each line.cells as cell}
                  {#if cell}
                    <td class="heat r" style="--heat: {cell.heat}">{cell.percent}<small>{cell.count}</small></td>
                  {:else}
                    <td class="heat"></td>
                  {/if}
                {/each}
              </tr>
            {/each}
          </tbody>
        </Table>
      {:else}
        <div class="chart-box"><EmptyState title={t('admin.analytics.noRows')} /></div>
      {/if}
    </Card>
  {/if}
{/snippet}

{#snippet heading(id: string)}
  <div class="heading">
    <Heading level={2} variant="section">{t(`admin.analytics.section.${id}.title`)}</Heading>
    <Text as="p" size="sm" tone="muted">{t(`admin.analytics.section.${id}.question`)}</Text>
  </div>
{/snippet}

<section class="screen active analytics">
  <PageHead eyebrow={t('admin.analytics.eyebrow')} title={t('admin.analytics.title')} description={t('admin.analytics.description')}>
    {#snippet trailing()}
      <SegmentedControl options={labels} label={t('admin.analytics.windowLabel')} bind:value={() => current, pick} />
    {/snippet}
  </PageHead>

  <Text as="p" size="sm" tone="muted">
    {view ? t('admin.analytics.period', view.period) : ''} {t('admin.analytics.exactNote')}
  </Text>

  <SectionNav label={t('admin.analytics.nav.label')} items={navItems} orientation="horizontal" />

  {#if degraded}<AlertBanner>{t('admin.analytics.degraded')}</AlertBanner>{/if}
  {#if view?.empty}<EmptyState title={t('admin.analytics.empty')} />{/if}

  <section id="overview" class="block">
    {@render heading('overview')}
    <div class="kpis">
      {#if view}
        {#each view.kpis as kpi (kpi.key)}
          <StatTile
            label={t(`admin.analytics.kpi.${kpi.key}`)}
            value={kpi.value}
            unit={kpi.unit ? phrase(kpi.unit) : undefined}
            delta={phrase(kpi.delta)}
            flat={kpi.trend !== 'up'}
          />
        {/each}
      {:else}
        {#each Array(8) as _, slot (slot)}<Skeleton variant="block" height="112px" />{/each}
      {/if}
    </div>
  </section>

  <section id="growth" class="block">
    {@render heading('growth')}
    {@render seriesCard('signups', view?.growth.signups)}
    {@render gainLossCard(view?.growth.gainLoss)}
    <Grid cols={3} stackAt="md">
      {@render barCard('funnel', view?.growth.funnel)}
      {@render barCard('churn', view?.growth.churn, view?.growth.churnFacts)}
      {@render barCard('presets', view?.growth.presets)}
    </Grid>
  </section>

  <section id="engagement" class="block">
    {@render heading('engagement')}
    <Grid cols={2} stackAt="md">
      {@render seriesCard('active', view?.engagement.active)}
      {@render seriesCard('live', view?.engagement.live)}
      {@render seriesCard('messages', view?.engagement.messages)}
      {@render seriesCard('answered', view?.engagement.answered)}
    </Grid>
    {@render factsCard('activity', view?.engagement.facts)}
  </section>

  <section id="retention" class="block">
    {@render heading('retention')}
    {@render cohortCard()}
  </section>

  <section id="revenue" class="block">
    {@render heading('revenue')}
    <div class="thirds">
      <div class="cell span-2">{@render tableCard('plans', view?.revenue.plans, undefined, view?.revenue.billingFacts)}</div>
      <div class="cell">{@render barCard('sources', view?.revenue.sources)}</div>
      <div class="cell">{@render barCard('billing', view?.revenue.billing)}</div>
      <div class="cell span-2">{@render tableCard('conversion', view?.revenue.conversion)}</div>
    </div>
  </section>

  <section id="features" class="block">
    {@render heading('features')}
    {@render tableCard('modules', view?.features.modules, '960px')}
    <Grid cols={2} stackAt="md">
      {@render tableCard('breadth', view?.features.breadth, undefined, view?.features.breadthFacts)}
      <div class="column">
        {@render barCard('integrations', view?.features.integrations)}
        {@render barCard('pagesHidden', view?.features.pagesHidden)}
      </div>
    </Grid>
  </section>

  <section id="commands" class="block">
    {@render heading('commands')}
    {@render tableCard('builtIn', view?.commands.builtIn)}
    <Grid cols={3} stackAt="md">
      {@render factsCard('custom', view?.commands.custom)}
      {@render barCard('perms', view?.commands.perms)}
      {@render barCard('flags', view?.commands.flags)}
    </Grid>
  </section>

  <section id="audience" class="block">
    {@render heading('audience')}
    {@render barCard('locales', view?.audience.locales)}
  </section>
</section>

<style>
  .analytics {
    display: grid;
    gap: var(--bb-space-7);
  }
  .block {
    display: grid;
    gap: var(--bb-space-4);
    scroll-margin-top: var(--bb-space-9);
  }
  .heading {
    display: grid;
    gap: var(--bb-space-2);
  }
  .thirds {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--bb-space-4);
  }
  .cell {
    display: grid;
    min-width: 0;
  }
  .span-2 {
    grid-column: span 2;
  }
  .column {
    display: grid;
    gap: var(--bb-space-4);
    min-width: 0;
  }
  .axis {
    display: flex;
    justify-content: space-between;
    gap: var(--bb-space-3);
  }
  .kpis {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: var(--bb-space-4);
  }
  .chart-box {
    display: grid;
    align-items: center;
    min-height: 178px;
  }
  @media (max-width: 900px) {
    .thirds {
      grid-template-columns: 1fr;
    }
    .span-2 {
      grid-column: auto;
    }
    .kpis {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
</style>
