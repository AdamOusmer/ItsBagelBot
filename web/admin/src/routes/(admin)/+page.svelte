<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import OverviewGrid from '@bagel/ui/svelte/OverviewGrid.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import OverviewMetrics from '#lib/components/overview/OverviewMetrics.svelte';
  import GiveawayAlerts from '#lib/components/overview/GiveawayAlerts.svelte';
  import EnrollmentPanel from '#lib/components/overview/EnrollmentPanel.svelte';
  import FleetPanel from '#lib/components/overview/FleetPanel.svelte';
  import HealthPanel from '#lib/components/overview/HealthPanel.svelte';
  import FeedPanel from '#lib/components/overview/FeedPanel.svelte';
  import AuditPeek from '#lib/components/overview/AuditPeek.svelte';
  import QuickActions from '#lib/components/overview/QuickActions.svelte';
  import BotCard from '#lib/components/overview/BotCard.svelte';

  let { data } = $props();

  const { t } = getI18n();
</script>

<section class="screen active">
  <PageHead compact description={t('admin.overview.description')}>
    {t('admin.overview.titlePre')}<em>{t('admin.overview.titleEm')}</em>
  </PageHead>

  <OverviewMetrics accounts={data.accountStats} live={data.liveCount} health={data.health} />

  <OverviewGrid>
    {#snippet main()}
      <EnrollmentPanel enrollment={data.enrollment} days={data.days} />
      <QuickActions canNotify={data.canNotify} />
    {/snippet}

    {#snippet side()}
      {#if data.canReadAudit}
        <div class="slot slot-detail">
          {#await data.audit}
            <Skeleton variant="block" height="100%" />
          {:then p}
            <AuditPeek entries={p.value} ok={p.ok} />
          {/await}
        </div>
      {/if}

      <div class="slot slot-detail">
        {#await data.feed}
          <Skeleton variant="block" height="100%" />
        {:then p}
          <FeedPanel board={p.value} ok={p.ok} />
        {/await}
      </div>
    {/snippet}
  </OverviewGrid>

  <OverviewGrid>
    {#snippet main()}
      <div class="slot slot-operations">
        {#await data.fleet}
          <Skeleton variant="block" height="100%" />
        {:then p}
          <FleetPanel snapshot={p.value} ok={p.ok} trialRead={data.trials} showTrials={data.canViewTrials} />
        {/await}
      </div>
    {/snippet}

    {#snippet side()}
      <div class="slot slot-operations">
        {#await data.health}
          <Skeleton variant="block" height="100%" />
        {:then p}
          <HealthPanel probes={p.value} ok={p.ok} />
        {/await}
      </div>
    {/snippet}
  </OverviewGrid>

  {#if data.canViewGiveaways || data.canLinkBot}
    <div class="tools">
      <Grid min="24rem">
        {#if data.canViewGiveaways}
          <div class="slot" class:slot-tool={data.canLinkBot} class:slot-alerts={!data.canLinkBot}>
            {#await data.giveawayAlerts}
              <Skeleton variant="block" height="100%" />
            {:then p}
              <GiveawayAlerts alerts={p.value} ok={p.ok} />
            {/await}
          </div>
        {/if}

        {#if data.canLinkBot}
          <div class="slot slot-tool bot-slot">
            {#await data.bot}
              <Skeleton variant="block" height="100%" />
            {:then p}
              <BotCard present={p.value} botLink={data.botLink} />
            {/await}
          </div>
        {/if}
      </Grid>
    </div>
  {/if}

</section>

<style>
  .slot {
    display: grid;
    grid-template: minmax(0, 1fr) / minmax(0, 1fr);
  }

  .tools {
    margin-block-end: var(--row-gap);
  }

  .slot-alerts {
    block-size: 11rem;
  }

  .slot-operations {
    block-size: 14rem;
  }

  .slot-detail {
    block-size: 17.5rem;
  }

  .bot-slot {
    container-type: inline-size;
  }

  .slot-tool {
    block-size: 17rem;
  }

  @media (max-width: 520px) {
    .slot-tool {
      block-size: 19rem;
    }
  }
</style>
