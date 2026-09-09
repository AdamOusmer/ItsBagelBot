<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The overview's heaviest panel, and what is left of the deleted /analytics
  // route: a window selector, the signups curve, and the three totals that
  // answer "is growth moving" without leaving the page.
  //
  // The curve is the shared AreaSeries, not the old page-local EnrollmentChart.
  // That component drew a second panel (a cumulative "registered" line derived
  // BACKWARDS from today's total) which was approximate by construction --
  // deletions make every historical point a guess, and it carried an "est."
  // label saying so. A stat tile with the live total is the honest version of
  // the same answer, so the derived line went with the component.
  import Card from '@bagel/shared/components/Card.svelte';
  import CardHead from '@bagel/shared/components/CardHead.svelte';
  import SegmentedControl from '@bagel/shared/components/SegmentedControl.svelte';
  import StatTile from '@bagel/shared/components/StatTile.svelte';
  import AreaSeries from '@bagel/shared/components/AreaSeries.svelte';
  import EmptyState from '@bagel/shared/components/EmptyState.svelte';
  import { getI18n } from '@bagel/shared/i18n/context';
  import type { EnrollmentWire } from '$lib/server/services';
  import { ENROLLMENT_WINDOWS, type EnrollmentWindow } from '$lib/enrollment-window';

  let {
    enrollment,
    days,
    onWindow
  }: {
    enrollment: EnrollmentWire;
    days: EnrollmentWindow;
    onWindow: (next: EnrollmentWindow) => void;
  } = $props();

  const { t } = getI18n();

  const WINDOW_LABEL = {
    7: 'admin.overview.window7',
    30: 'admin.overview.window30',
    90: 'admin.overview.window90'
  } as const;

  // SegmentedControl keys its options by their displayed string, so the labels
  // are the option set and the pick is resolved back by index. Function binding
  // (get/set) rather than a plain bind: the URL owns the window, so a click has
  // to navigate instead of writing local state the next load would contradict.
  const labels = $derived(ENROLLMENT_WINDOWS.map((d) => t(WINDOW_LABEL[d])));
  const current = $derived(labels[ENROLLMENT_WINDOWS.indexOf(days)] ?? labels[1]);

  function pick(label: string) {
    const i = labels.indexOf(label);
    if (i >= 0) onWindow(ENROLLMENT_WINDOWS[i]);
  }

  const buckets = $derived(enrollment.days);
  const values = $derived(buckets.map((d) => d.count));
  const stats = $derived(enrollment.stats);
  const today = $derived(buckets.length ? buckets[buckets.length - 1].count : 0);
  const week = $derived(buckets.slice(-7).reduce((sum, d) => sum + d.count, 0));
</script>

<Card as="section">
  <CardHead title={t('admin.overview.enrollmentTitle')}>
    {#snippet action()}
      <a class="more" href="/users">{t('admin.overview.allUsers')}</a>
    {/snippet}
  </CardHead>

  <div class="window">
    <SegmentedControl
      options={labels}
      label={t('admin.overview.enrollmentWindow')}
      bind:value={() => current, pick}
    />
  </div>

  {#if values.length > 1}
    <AreaSeries {values} ariaLabel={t('admin.overview.enrollmentChart', { days: String(days) })} />
  {:else}
    <EmptyState title={t('admin.overview.enrollmentEmpty')} />
  {/if}

  <div class="tiles">
    <StatTile
      label={t('admin.overview.statTotal')}
      value={stats.total_users.toLocaleString()}
      delta={t('admin.overview.statTotalDelta', { active: stats.active_users.toLocaleString() })}
      flat
    />
    <StatTile
      label={t('admin.overview.statToday')}
      value={today.toLocaleString()}
      delta={t('admin.overview.statTodayDelta')}
      flat
    />
    <StatTile
      label={t('admin.overview.statWeek')}
      value={week.toLocaleString()}
      delta={t('admin.overview.statWeekDelta', { premium: stats.premium_users.toLocaleString() })}
      flat
    />
  </div>
</Card>

<style>
  .window {
    margin-bottom: 14px;
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: calc(14px * var(--d, 1));
    margin-top: 16px;
  }
  @media (max-width: 700px) {
    .tiles {
      grid-template-columns: 1fr;
    }
  }
</style>
