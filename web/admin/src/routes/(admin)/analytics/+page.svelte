<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { goto, invalidate } from '$app/navigation';
  import { page } from '$app/state';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import SectionNav from '@bagel/ui/svelte/SectionNav.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { livePoll } from '@bagel/kit/live-poll';
  import { ANALYTICS_DATA, ANALYTICS_REFRESH_MS } from '#lib/analytics-page';
  import { applyStreamed } from '#lib/streamed-load';
  import AnalyticsAudience from '#lib/components/analytics/AnalyticsAudience.svelte';
  import AnalyticsCommands from '#lib/components/analytics/AnalyticsCommands.svelte';
  import AnalyticsCommunity from '#lib/components/analytics/AnalyticsCommunity.svelte';
  import AnalyticsEngagement from '#lib/components/analytics/AnalyticsEngagement.svelte';
  import AnalyticsFeatures from '#lib/components/analytics/AnalyticsFeatures.svelte';
  import AnalyticsGrowth from '#lib/components/analytics/AnalyticsGrowth.svelte';
  import AnalyticsOverview from '#lib/components/analytics/AnalyticsOverview.svelte';
  import AnalyticsRecipes from '#lib/components/analytics/AnalyticsRecipes.svelte';
  import AnalyticsReliability from '#lib/components/analytics/AnalyticsReliability.svelte';
  import AnalyticsRetention from '#lib/components/analytics/AnalyticsRetention.svelte';
  import AnalyticsRevenue from '#lib/components/analytics/AnalyticsRevenue.svelte';
  import type { PageProps } from './$types';

  let { data }: PageProps = $props();
  const { locale, t } = getI18n();
  const formatDate = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format;
  const sections = ['overview', 'growth', 'engagement', 'retention', 'revenue', 'features', 'community', 'recipes', 'reliability', 'commands', 'audience'] as const;

  type Snapshot<T> = { days: number; value: T };
  let analytics = $state.raw<Snapshot<Awaited<PageProps['data']['analytics']>>>();
  let ledger = $state.raw<Snapshot<Awaited<PageProps['data']['donations']>>>();
  const active = $derived(analytics?.days === data.days ? analytics.value : undefined);
  const donations = $derived(ledger?.days === data.days ? ledger.value : undefined);
  const view = $derived(active?.view);
  const period = $derived({
    start: formatDate(new Date(data.period.from)),
    end: formatDate(new Date(data.period.to))
  });

  const windowOptions = $derived(data.windows.map((days) => ({
    value: String(days),
    label: days === 1 ? t('admin.analytics.daily') : t('admin.analytics.window', { days })
  })));
  const sectionOptions = $derived(sections.map((value) => ({ value, label: t(`admin.analytics.nav.${value}`) })));
  const navItems = $derived(sectionOptions.map(({ value, label }) => ({ href: `#${value}`, label })));

  $effect(() => {
    const days = data.days;
    applyStreamed(data.analytics, (value) => { analytics = { days, value }; });
  });

  $effect(() => {
    const days = data.days;
    applyStreamed(data.donations, (value) => { ledger = { days, value }; });
  });

  async function refresh() {
    if (document.hidden) return false;
    await invalidate(ANALYTICS_DATA)
      .then(() => Promise.all([data.analytics, data.donations]))
      .catch(() => undefined);
    return false;
  }

  $effect(() => livePoll(refresh, {
    firstDelayMs: ANALYTICS_REFRESH_MS,
    delayMs: () => ANALYTICS_REFRESH_MS,
    hiddenDelayMs: ANALYTICS_REFRESH_MS,
    refreshOnVisible: true,
    timeoutMs: Infinity
  }));

  function pick(days: string) {
    if (days === String(data.days)) return;
    const url = new URL(page.url.href);
    url.hash = '';
    if (days === String(data.defaultDays)) url.searchParams.delete('days');
    else url.searchParams.set('days', days);
    void goto(url, { reset: false })
      .then(() => requestAnimationFrame(focusWindow), () => undefined);
  }

  function focusWindow() {
    document.querySelector<HTMLElement>('#analytics-window [aria-checked="true"]')?.focus({ preventScroll: true });
  }

  function jump(section: string) {
    const url = new URL(page.url.href);
    url.hash = section;
    void goto(url);
  }
</script>

<section class="screen active">
  <Stack gap={6}>
    <div>
      <PageHead compact description={t('admin.analytics.description')}>
        {t('admin.analytics.titlePre')}<em>{t('admin.analytics.titleEm')}</em>
      </PageHead>
      <PageToolbar>
        {#snippet leading()}
          <div class="window-picker">
            <SegmentedControl id="analytics-window" options={windowOptions} label={t('admin.analytics.windowLabel')} value={String(data.days)} onValueChange={pick} even />
          </div>
        {/snippet}
        {#snippet trailing()}
          <div class="period" aria-live="polite">
            <Text as="p" size="sm" tone="muted">
              <bb-fit><span data-fit>{t('admin.analytics.period', period)}</span></bb-fit>
            </Text>
          </div>
        {/snippet}
      </PageToolbar>
      <Text as="p" size="sm" tone="muted">{t('admin.analytics.exactNote')}</Text>
    </div>
    <div class="section-links">
      <SectionNav label={t('admin.analytics.nav.label')} items={navItems} orientation="horizontal" />
    </div>
    <div class="section-picker">
      <Select
        label={t('admin.analytics.nav.label')}
        placeholder={t('admin.analytics.jumpToSection')}
        options={sectionOptions}
        value={page.url.hash.slice(1)}
        onchange={(event) => void jump(event.currentTarget.value)}
        fill
      />
    </div>
    <AnalyticsOverview metrics={view?.kpis} degraded={active?.degraded} empty={view?.empty} />
    <AnalyticsGrowth growth={view?.growth} />
    <AnalyticsEngagement engagement={view?.engagement} />
    <AnalyticsRetention retention={view?.retention} />
    <AnalyticsRevenue revenue={view?.revenue} donations={donations?.view} donationsError={donations?.degraded} />
    <AnalyticsFeatures features={view?.features} />
    <AnalyticsCommunity community={view?.community} />
    <AnalyticsRecipes recipes={view?.recipes} />
    <AnalyticsReliability reliability={view?.reliability} />
    <AnalyticsCommands commands={view?.commands} />
    <AnalyticsAudience audience={view?.audience} />
  </Stack>
</section>

<style>
  .window-picker {
    inline-size: min(100%, 30rem);
    min-inline-size: 0;
  }
  .period {
    min-inline-size: 0;
    inline-size: min(100%, 20rem);
    min-block-size: 1.5rem;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-items: center;
  }
  .section-picker { display: none; }
  @media (max-width: 760px) {
    .section-links { display: none; }
    .section-picker { display: block; }
    .window-picker, .period { flex-basis: 100%; }
  }
</style>
