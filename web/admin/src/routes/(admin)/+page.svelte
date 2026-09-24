<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { goto } from '$app/navigation';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import OverviewGrid from '@bagel/ui/svelte/OverviewGrid.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import EnrollmentPanel from '$lib/components/overview/EnrollmentPanel.svelte';
  import FleetPanel from '$lib/components/overview/FleetPanel.svelte';
  import HealthPanel from '$lib/components/overview/HealthPanel.svelte';
  import AuditPeek from '$lib/components/overview/AuditPeek.svelte';
  import QuickActions from '$lib/components/overview/QuickActions.svelte';
  import BotCard from '$lib/components/overview/BotCard.svelte';
import type { EnrollmentWindow } from '$lib/enrollment-window';
  import StatePill from '$lib/components/StatePill.svelte';

  let { data } = $props();

  const { t } = getI18n();

  function setWindow(days: EnrollmentWindow) {
    goto(days === 30 ? '/' : `/?days=${days}`, { keepFocus: true, noScroll: true });
  }
</script>

<section class="screen active">
  <PageHead
    eyebrow={t('admin.overview.eyebrow')}
    description={t('admin.overview.description')}
  >
    {t('admin.overview.titlePre')}<em>{t('admin.overview.titleEm')}</em>
  </PageHead>

  <OverviewGrid>
    {#snippet main()}
      {#await data.enrollment}
        <SkeletonStack rows={1} height="420px" />
      {:then p}
        {#if !p.ok}
          <AlertBanner>{t('admin.overview.degraded')}</AlertBanner>
        {/if}
        <EnrollmentPanel enrollment={p.value} days={data.days} onWindow={setWindow} />
      {/await}

      {#await data.fleet}
        <SkeletonStack rows={1} height="260px" />
      {:then p}
        <FleetPanel snapshot={p.value} ok={p.ok} trialRead={data.trials} showTrials={data.canViewTrials} />
      {/await}
    {/snippet}

    {#snippet side()}
      {#await data.health}
        <SkeletonStack rows={1} height="240px" />
      {:then p}
        <HealthPanel probes={p.value} ok={p.ok} />
      {/await}

      {#if data.canReadAudit}
        {#await data.audit}
          <SkeletonStack rows={1} height="220px" />
        {:then p}
          <AuditPeek entries={p.value} />
        {/await}
      {/if}

      <QuickActions canNotify={data.canNotify} />

      {#if data.canLinkBot}
        {#await data.bot}
          <Skeleton variant="block" height="160px" />
        {:then p}
          <BotCard present={p.value} />
        {/await}
      {/if}

      {#await data.giveawayAlerts}
        <SkeletonStack rows={1} height="120px" />
      {:then p}
        {#if !p.ok}
          <AlertBanner>{t('admin.giveaways.alertsUnavailable')}</AlertBanner>
        {:else if p.value.length}
          <section class="pending-awards" aria-labelledby="pending-awards-title">
            <div class="pending-head"><h2 id="pending-awards-title">{t('admin.giveaways.alerts')}</h2><StatePill tone="warning">{p.value.length}</StatePill></div>
            {#each p.value.slice(0, 4) as alert (alert.id)}
              <a href="/giveaways"><strong>{alert.awardId}</strong><span>{alert.reason}</span></a>
            {/each}
          </section>
        {/if}
      {/await}
    {/snippet}
  </OverviewGrid>
</section>

<style>
  .pending-awards { padding:16px; border:1px solid rgba(242,200,121,.35); border-radius:14px; background:rgba(242,200,121,.06); }
  .pending-head { display:flex; align-items:center; justify-content:space-between; gap:12px; margin-bottom:10px; }
  .pending-head h2 { margin:0; font-size:14px; }
  .pending-awards a { display:block; padding:9px 0; border-top:1px solid var(--bb-border); color:inherit; text-decoration:none; }
  .pending-awards a:hover strong { color:var(--bb-tan-pale); }
  .pending-awards span { display:block; color:var(--bb-muted); font-size:12px; margin-top:3px; }
</style>
