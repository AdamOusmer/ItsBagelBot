<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { clockFace, type ActivityFeed, type ActivityKind } from '$lib/overview-live';

  const { t, locale } = getI18n();

  let { feed, stale = false }: { feed: ActivityFeed; stale?: boolean } = $props();

  const KIND_LABEL: Record<ActivityKind, string> = {
    command: 'overview.kindCommand',
    timer: 'overview.kindTimer',
    automod: 'overview.kindAutomod',
    reward: 'overview.kindReward',
    loyalty: 'overview.kindLoyalty',
    event: 'overview.kindEvent',
    queue: 'overview.kindQueue'
  };

  type KindTone = 'success' | 'danger' | 'warning' | 'info' | 'neutral';

  const KIND_TONE: Record<ActivityKind, KindTone> = {
    command: 'success',
    timer: 'warning',
    automod: 'danger',
    reward: 'warning',
    loyalty: 'neutral',
    event: 'info',
    queue: 'neutral'
  };

  const footer = $derived.by(() => {
    const parts: string[] = [];
    if (feed.medianMs !== null) parts.push(t('overview.feedMedian', { ms: feed.medianMs }));
    parts.push(
      feed.dropped > 0
        ? t('overview.feedDropped', { n: feed.dropped })
        : t('overview.feedNothingDropped')
    );
    return parts.join(' · ');
  });
</script>

<Card as="section" flush aria-labelledby="ov-log-h">
  <div class="ov-log__head">
    <Heading level={6} as="h2" variant="title" id="ov-log-h">{t('overview.botJustDid')}</Heading>
    {#if feed.ok && feed.rows.length}
      {#if stale}
        <Tag tone="quiet" mark="hollow">{t('overview.feedReconnecting')}</Tag>
      {:else}
        <Tag tone="incoming" mark="plus" sweep>{t('overview.feedLive')}</Tag>
      {/if}
    {/if}
  </div>

  {#if !feed.ok}
    <div class="ov-log__empty"><Text size="sm" tone="muted">{t('overview.feedUnavailable')}</Text></div>
  {:else if !feed.rows.length}
    <div class="ov-log__empty"><Text size="sm" tone="muted">{t('overview.feedEmpty')}</Text></div>
  {:else}
    <ul class="ov-log__list" class:ov-log__list--stale={stale}>
      {#each feed.rows as row (row.id)}
        <li class="ov-log__row">
          <span class="ov-log__time"><Text as="span" size="xs" mono tone="muted">{clockFace(row.at, locale)}</Text></span>
          <span class="ov-log__kind"><Tag tone={KIND_TONE[row.kind]} bare>{t(KIND_LABEL[row.kind])}</Tag></span>
          <span class="ov-log__text"><Text as="span" size="sm" truncate>{row.text}</Text></span>
          <span class="ov-log__meta"><Text as="span" size="xs" mono tone="muted">{row.meta}</Text></span>
        </li>
      {/each}
    </ul>
    <div class="ov-log__foot">
      <Label mono as="span">{footer}</Label>
      <TextLink href="/commands" label={`${t('overview.fullLog')} →`} />
    </div>
  {/if}
</Card>

<style>
  .ov-log__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 17px 20px 14px;
    border-bottom: 1px solid var(--bb-border);
  }
  .ov-log__list {
    list-style: none;
    margin: 0;
    padding: 0;
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .ov-log__list--stale {
    opacity: 0.55;
  }
  .ov-log__row {
    display: flex;
    align-items: center;
    gap: 13px;
    padding: 11px 20px;
    border-bottom: 1px solid rgba(var(--bb-tan-rgb), 0.09);
    animation: ov-feed-in var(--bb-dur-base) var(--bb-ease-out-expo) both;
  }
  .ov-log__time {
    flex: none;
    width: 4.75rem;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .ov-log__kind {
    flex: none;
    display: flex;
    justify-content: center;
    min-width: 78px;
  }
  .ov-log__text {
    flex: 1;
    min-width: 0;
  }
  .ov-log__meta {
    flex: none;
  }
  .ov-log__foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 13px 20px;
  }
  .ov-log__empty {
    padding: 18px 20px 20px;
  }

  @media (max-width: 640px) {
    .ov-log__meta {
      display: none;
    }
    .ov-log__kind {
      min-width: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .ov-log__row {
      animation: none;
    }
  }
</style>
