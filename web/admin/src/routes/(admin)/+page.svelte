<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Operator overview on the shared OverviewGrid: one wide column for the two
  // panels an operator reads first (growth, then the fleet), a rail for the
  // reads that only matter when something is wrong.
  //
  // Every panel is its own `{#await}` over its own streamed promise. The page
  // shell (head, grid, headings) is therefore never behind NATS, and a slow
  // responder costs one skeleton rather than the whole board -- which is what
  // the previous single-bundle load did.
  import { goto } from '$app/navigation';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import OverviewGrid from '@bagel/kit/components/OverviewGrid.svelte';
  import PageHead from '@bagel/kit/components/PageHead.svelte';
  import AlertBanner from '@bagel/kit/components/AlertBanner.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import EnrollmentPanel from '$lib/components/overview/EnrollmentPanel.svelte';
  import FleetPanel from '$lib/components/overview/FleetPanel.svelte';
  import HealthPanel from '$lib/components/overview/HealthPanel.svelte';
  import AuditPeek from '$lib/components/overview/AuditPeek.svelte';
  import QuickActions from '$lib/components/overview/QuickActions.svelte';
  import BotCard from '$lib/components/overview/BotCard.svelte';
  import type { EnrollmentWindow } from '$lib/enrollment-window';

  let { data } = $props();

  const { t } = getI18n();

  // The window lives in the URL (see +page.server.ts). keepFocus so the segment
  // the operator just pressed keeps the ring; the panel re-renders around it.
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
        <FleetPanel snapshot={p.value} ok={p.ok} />
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
    {/snippet}
  </OverviewGrid>
</section>
