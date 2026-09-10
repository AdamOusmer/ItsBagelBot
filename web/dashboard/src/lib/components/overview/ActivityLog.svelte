<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // "What the bot just did": the newest work the bot has done this stream.
  //
  // Rows are keyed by id so Svelte moves the existing nodes when a new row is
  // pushed onto the head instead of recreating the list; only the arriving row
  // animates. Keying by index would re-run the entry animation on every row at
  // once, every time.
  //
  // The footer states the median answer time AND whether anything was shed. The
  // pipeline hook drops events under backpressure by design, so a feed that
  // silently omitted them would be claiming a completeness it does not have.
  import { getI18n } from '@bagel/shared/i18n/context';
  import { clockFace, type ActivityFeed, type ActivityKind } from '$lib/overview-live';

  const { t } = getI18n();

  let { feed }: { feed: ActivityFeed } = $props();

  const KIND_LABEL: Record<ActivityKind, string> = {
    command: 'overview.kindCommand',
    timer: 'overview.kindTimer',
    automod: 'overview.kindAutomod',
    reward: 'overview.kindReward',
    loyalty: 'overview.kindLoyalty',
    event: 'overview.kindEvent',
    queue: 'overview.kindQueue'
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

<section class="ov-log" aria-labelledby="ov-log-h">
  <div class="ov-log__head">
    <h2 id="ov-log-h" class="ov-log__h">{t('overview.botJustDid')}</h2>
    {#if feed.ok && feed.rows.length}
      <span class="bb-tag bb-tag--incoming">
        <i class="bb-mark bb-mark--plus" aria-hidden="true"></i>
        {t('overview.feedLive')}
        <i class="bb-sweep" aria-hidden="true"></i>
      </span>
    {/if}
  </div>

  {#if !feed.ok}
    <p class="ov-log__empty">{t('overview.feedUnavailable')}</p>
  {:else if !feed.rows.length}
    <p class="ov-log__empty">{t('overview.feedEmpty')}</p>
  {:else}
    <ul class="ov-log__list">
      {#each feed.rows as row (row.id)}
        <li class="ov-log__row">
          <span class="ov-log__time">{clockFace(row.at)}</span>
          <span class="bb-tag bb-tag--bare ov-log__chip ov-log__chip--{row.kind}">{t(KIND_LABEL[row.kind])}</span>
          <span class="ov-log__text">{row.text}</span>
          <span class="ov-log__meta">{row.meta}</span>
        </li>
      {/each}
    </ul>
    <div class="ov-log__foot">
      <span>{footer}</span>
      <a href="/commands">{t('overview.fullLog')} →</a>
    </div>
  {/if}
</section>

<style>
  .ov-log {
    background: var(--bb-card-bg);
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-md);
    overflow: hidden;
  }
  .ov-log__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 17px 20px 14px;
    border-bottom: 1px solid var(--bb-border);
  }
  .ov-log__h {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0;
  }
  /* .ov-log__live/.ov-dot--live (glowing ov-pulse dot) dropped for the global
     .bb-tag--incoming label; .bb-sweep carries the motion. */
  .ov-log__list {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .ov-log__row {
    display: flex;
    align-items: center;
    gap: 13px;
    padding: 11px 20px;
    border-bottom: 1px solid rgba(201, 168, 124, 0.09);
    animation: ov-feed-in 420ms cubic-bezier(0.16, 1, 0.3, 1) both;
  }
  .ov-log__time {
    flex: none;
    width: 46px;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    font-variant-numeric: tabular-nums;
  }
  /* Pill fill/border/radius gone: the row is already bordered, so the kind
     chip is a .bb-tag--bare label. Only layout + colour stay scoped here. */
  .ov-log__chip {
    flex: none;
    min-width: 78px;
    justify-content: center;
    font-size: 9.5px;
    letter-spacing: 0.12em;
    color: var(--bb-muted);
  }
  .ov-log__chip--command {
    color: var(--bb-status-success-fg);
  }
  .ov-log__chip--automod {
    color: var(--bb-status-error-fg);
  }
  .ov-log__chip--timer,
  .ov-log__chip--reward {
    color: var(--bb-status-warning-fg);
  }
  .ov-log__chip--event {
    color: var(--bb-status-info-fg);
  }
  .ov-log__text {
    flex: 1;
    min-width: 0;
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-white);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .ov-log__meta {
    flex: none;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-muted);
    font-variant-numeric: tabular-nums;
  }
  .ov-log__foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 13px 20px;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .ov-log__foot a {
    color: var(--bb-tan);
    text-decoration: none;
    letter-spacing: 0.12em;
  }
  .ov-log__foot a:hover {
    color: var(--bb-tan-pale);
  }
  .ov-log__empty {
    margin: 0;
    padding: 18px 20px 20px;
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-muted);
  }

  @media (max-width: 640px) {
    .ov-log__meta {
      display: none;
    }
    .ov-log__chip {
      min-width: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .ov-log__row {
      animation: none;
    }
  }
</style>
