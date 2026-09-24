<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import AreaSeries from '@bagel/ui/svelte/AreaSeries.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
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
      <a class="bb-card-head__more" href="/users">{t('admin.overview.allUsers')}</a>
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
