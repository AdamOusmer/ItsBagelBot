<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import FilterTabs from '#lib/components/FilterTabs.svelte';
  import { applyStreamed } from '#lib/streamed-load';
  import { CAMPAIGN_STATUS, campaignCounts, giveawayDate, giveawayPrize, type CampaignFilter } from '#lib/giveaway-workflow';
  import type { GiveawayWire } from '#lib/server/giveaways';

  type History = { giveaways: GiveawayWire[]; degraded: boolean };
  let { history }: { history: Promise<History> } = $props();

  const { locale, t } = getI18n();
  const number = new Intl.NumberFormat(locale);
  const formatter = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' });
  const statuses = Object.keys(CAMPAIGN_STATUS) as CampaignFilter[];

  let result = $state.raw<History>();
  let filter = $state<CampaignFilter>('all');
  const giveaways = $derived(result?.giveaways ?? []);
  const counts = $derived(campaignCounts(giveaways));
  const filtersDisabled = $derived(!result || result.degraded);
  const filterOptions = $derived(statuses.map((value) => ({
    value,
    label: t(CAMPAIGN_STATUS[value].label),
    count: filtersDisabled ? '' : number.format(counts[value])
  })));
  const visible = $derived(filter === 'all' ? giveaways : giveaways.filter((giveaway) => giveaway.status === filter));

  $effect(() => applyStreamed(history, (value) => { result = value; }));
</script>

<Stack gap={3}>
  <FilterTabs
    options={filterOptions}
    bind:value={() => filter, (value) => (filter = value as CampaignFilter)}
    label={t('admin.giveaways.state')}
    disabled={filtersDisabled}
    countWidth="3ch"
  />
  <PageToolbar>
    {#snippet trailing()}
      <ButtonLink href="#giveaway-draft" variant="secondary" block style="--btn-w: min(100%, 12rem)">
        <bb-fit><span data-fit>{t('admin.giveaways.create')}</span></bb-fit>
      </ButtonLink>
    {/snippet}
  </PageToolbar>
</Stack>

<DeckList>
  <div class="history-head">
    <CardHead title={t('admin.giveaways.history')} headingLevel={2} />
  </div>
  <div class="history-body" aria-busy={!result}>
    {#if !result}
      {#each [0, 1, 2, 3] as row (row)}
        <div class="history-placeholder" aria-hidden="true">
          <Skeleton width="36%" lines={2} />
          <Skeleton variant="pill" width="4.5rem" />
        </div>
      {/each}
    {:else if result.degraded}
      <div class="history-error"><AlertBanner tone="danger" flush>{t('admin.giveaways.degraded')}</AlertBanner></div>
    {:else if visible.length === 0}
      <EmptyState title={t(giveaways.length ? 'admin.giveaways.noMatches' : 'admin.giveaways.empty')} />
    {:else}
      {#key filter}
        <ul class="campaigns">
          {#each visible as giveaway (giveaway.id)}
            <ManagementRow
              as="li"
              href={`/giveaways/${encodeURIComponent(giveaway.id)}`}
              title={giveaway.title}
              meta={`${giveawayPrize(giveaway, number, t)} · ${giveawayDate(giveaway.createdAt, formatter, t)}`}
            >
              {#snippet marks()}
                <span class="campaign-status">
                  <Badge shape="status" tone={CAMPAIGN_STATUS[giveaway.status].tone}><bb-fit><span data-fit>{t(CAMPAIGN_STATUS[giveaway.status].label)}</span></bb-fit></Badge>
                </span>
              {/snippet}
            </ManagementRow>
          {/each}
        </ul>
      {/key}
    {/if}
  </div>
</DeckList>

<style>
  .history-head {
    padding: var(--bb-space-3) var(--bb-space-4);
    border-block-end: 1px solid var(--bb-border);
  }

  .history-body {
    min-block-size: 14.75rem;
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
    inline-size: 6rem;
  }
</style>
