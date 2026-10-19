<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
	import '@bagel/ui/styles/elements/feed.css';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import { formatCounterValue } from '@bagel/kit/validation';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { FeedBoard } from '$lib/server/services';

  let { board, ok }: { board: FeedBoard; ok: boolean } = $props();

  const { t, locale } = getI18n();

  const count = (raw: string) => formatCounterValue(raw, locale);
</script>

<Card as="section">
  <CardHead title={t('admin.overview.feedTitle')} />

  {#if !ok}
    <EmptyState title={t('admin.overview.feedUnavailable')} />
  {:else}
    <StatTile
      label={t('admin.overview.feedTotal')}
      value={count(board.total)}
      delta={t('admin.overview.feedRanked', { count: count(board.ranked) })}
      flat
    />

    {#if board.entries.length}
      <ol class="bb-node-list board">
        {#each board.entries as entry, i (entry.id)}
          <li class="bb-node-list__row">
            <span class="rank">{i + 1}</span>
            <span class="bb-node-list__name">{entry.name || t('admin.overview.feedUnnamed')}</span>
            <span class="bb-node-list__trail">{count(entry.count)}</span>
          </li>
        {/each}
      </ol>
    {:else}
      <EmptyState title={t('admin.overview.feedEmpty')} />
    {/if}
  {/if}
</Card>

<style>
  .board {
    list-style: none;
    padding: 0;
    margin: 10px 0 0;
  }
  .rank {
    min-width: 1.5em;
    font-family: var(--bb-font-mono);
    font-size: var(--bb-text-xs);
    color: var(--bb-tan-light);
  }
</style>
