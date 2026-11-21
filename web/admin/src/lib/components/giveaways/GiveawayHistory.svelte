<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
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
  const visible = $derived(filter === 'all' ? giveaways : giveaways.filter((giveaway) => giveaway.status === filter));

  $effect(() => applyStreamed(history, (value) => { result = value; }));
</script>

<PageToolbar class="history-toolbar">
  {#snippet leading()}
    <div class="filters" role="group" aria-label={t('admin.giveaways.state')}>
      {#each statuses as status (status)}
        <Chip pressed={filter === status} disabled={!result || result.degraded} onclick={() => { filter = status; }}>
          <span class="filter-label"><bb-fit><span data-fit>{t(CAMPAIGN_STATUS[status].label)}</span></bb-fit></span>
          <span class="filter-count" aria-hidden={!result || result.degraded}>{result && !result.degraded ? number.format(counts[status]) : '·'}</span>
        </Chip>
      {/each}
    </div>
  {/snippet}
  {#snippet trailing()}
    <TextLink href="#giveaway-draft" variant="arrow">{t('admin.giveaways.create')}</TextLink>
  {/snippet}
</PageToolbar>

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
        <Scroller maxHeight="100%" role="region" tabindex={0} aria-label={t('admin.giveaways.history')}>
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
        </Scroller>
      {/key}
    {/if}
  </div>
</DeckList>

<style>
  :global(.history-toolbar) {
    flex-wrap: wrap;
  }

  .filters {
    display: grid;
    grid-template-columns: repeat(6, minmax(0, 1fr));
    gap: var(--bb-space-2);
    inline-size: min(100%, 42rem);
    min-inline-size: 0;
  }

  .filter-label {
    flex: 1;
    min-inline-size: 0;
  }

  .filter-count {
    min-inline-size: 3ch;
    text-align: end;
    font-variant-numeric: tabular-nums;
    opacity: 0.75;
  }

  .history-head {
    padding: var(--bb-space-3) var(--bb-space-4);
    border-block-end: 1px solid var(--bb-border);
  }

  .history-body {
    block-size: 14.75rem;
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

  @media (max-width: 1100px) {
    .filters {
      inline-size: 100%;
    }
  }

  @media (max-width: 820px) {
    .filters {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
  }

  @media (max-width: 460px) {
    .filters {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
</style>
