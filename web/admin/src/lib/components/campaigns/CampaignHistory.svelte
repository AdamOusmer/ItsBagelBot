<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import { giveawayDate } from '#lib/giveaway-workflow';
  import { CAMPAIGN_STATUS, dispatchedCount } from '#lib/campaign-workflow';
  import { applyStreamed } from '#lib/streamed-load';
  import type { CampaignWire } from '#lib/server/campaigns';

  type History = { campaigns: CampaignWire[]; degraded: boolean };
  let { history }: { history: Promise<History> } = $props();

  const { locale, t } = getI18n();
  const number = new Intl.NumberFormat(locale);
  const formatter = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' });

  let result = $state.raw<History>();

  $effect(() => applyStreamed(history, (value) => { result = value; }));

  function meta(campaign: CampaignWire): string {
    return t('admin.campaigns.listMeta', {
      date: giveawayDate(campaign.created_at, formatter, t),
      sent: number.format(dispatchedCount(campaign.sends ?? {}))
    });
  }
</script>

<DeckList>
  <div class="history-head">
    <CardHead title={t('admin.campaigns.history')} headingLevel={2} />
  </div>
  <div class="history-body" aria-busy={!result}>
    {#if !result}
      {#each [0, 1, 2] as row (row)}
        <div class="history-placeholder" aria-hidden="true">
          <Skeleton width="36%" lines={2} />
          <Skeleton variant="pill" width="4.5rem" />
        </div>
      {/each}
    {:else if result.degraded}
      <div class="history-error"><AlertBanner tone="danger" flush>{t('admin.campaigns.degraded')}</AlertBanner></div>
    {:else if result.campaigns.length === 0}
      <EmptyState title={t('admin.campaigns.empty')} />
    {:else}
      <ul class="campaigns">
        {#each result.campaigns as campaign (campaign.id)}
          <ManagementRow as="li" href={`/campaigns/${campaign.id}`} title={campaign.name} meta={meta(campaign)}>
            {#snippet marks()}
              <span class="campaign-status">
                <Badge shape="status" tone={CAMPAIGN_STATUS[campaign.status].tone}><bb-fit><span data-fit>{t(CAMPAIGN_STATUS[campaign.status].label)}</span></bb-fit></Badge>
              </span>
            {/snippet}
          </ManagementRow>
        {/each}
      </ul>
    {/if}
  </div>
</DeckList>

<style>
  .history-head {
    padding: var(--bb-space-3) var(--bb-space-4);
    border-block-end: 1px solid var(--bb-border);
  }

  .history-body {
    min-block-size: 11rem;
  }

  .history-placeholder {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--bb-space-3);
    block-size: 3.6875rem;
    padding-inline: var(--bb-space-4);
    border-block-end: 1px solid var(--bb-border);
  }

  .history-error {
    padding: var(--bb-space-3) var(--bb-space-4);
  }

  .campaigns {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .campaign-status {
    display: inline-grid;
    inline-size: 6.5rem;
  }
</style>
