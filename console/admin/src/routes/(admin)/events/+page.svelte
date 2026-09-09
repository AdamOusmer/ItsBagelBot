<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The ingress shard-lifecycle feed: a live, non-persistent SSE view of shard
  // connections, Conduit bindings and disconnect reasons.
  //
  // The stream itself is unchanged (/events/stream, one EventSource, the same
  // 300-event cap and pause buffer). What changed is the chrome: the feed now
  // scrolls inside a Scroller instead of growing the page without bound, the
  // pause control is the shared Switch rather than a Button whose label was the
  // state, and each row's dot is the shared StatusDot rather than a third copy
  // of the tone palette.
  //
  // There is no inspector: an event is three fields, all of them already on the
  // row, and it is gone on reload.
  import { onMount } from 'svelte';
  import PageHead from '@bagel/shared/components/PageHead.svelte';
  import PageToolbar from '@bagel/shared/components/PageToolbar.svelte';
  import SegmentedControl from '@bagel/shared/components/SegmentedControl.svelte';
  import SearchInput from '@bagel/shared/components/SearchInput.svelte';
  import Card from '@bagel/shared/components/Card.svelte';
  import CardHead from '@bagel/shared/components/CardHead.svelte';
  import Scroller from '@bagel/shared/components/Scroller.svelte';
  import Switch from '@bagel/shared/components/Switch.svelte';
  import Button from '@bagel/shared/components/Button.svelte';
  import EmptyState from '@bagel/shared/components/EmptyState.svelte';
  import type { StatusTone } from '@bagel/shared/status-tone';
  import { getI18n } from '@bagel/shared/i18n/context';
  import StatusDot from '$lib/components/StatusDot.svelte';

  interface FeedEvent {
    subject: string;
    label: string;
    tone: 'up' | 'down' | 'neutral';
    payload: string;
    time: string;
  }

  const { t } = getI18n();

  // The feed's own three-word tone vocabulary, mapped onto the shared one. A
  // lifecycle "up" is a shard that came back, which is the same success the
  // health panel's green means.
  const TONE_DOT: Record<FeedEvent['tone'], StatusTone> = {
    up: 'success',
    down: 'error',
    neutral: 'neutral'
  };

  const CAP = 300;
  let events = $state<FeedEvent[]>([]);
  // Honest connection state: EventSource retries forever, so distinguish "live"
  // from "reconnecting" instead of flashing a generic spinner.
  let conn = $state<'connecting' | 'live' | 'reconnecting'>('connecting');
  let paused = $state(false);
  let missedWhilePaused = $state(0);
  let buffer: FeedEvent[] = [];

  function receive(raw: string) {
    try {
      const ev = JSON.parse(raw) as FeedEvent;
      if (paused) {
        buffer = [ev, ...buffer].slice(0, CAP);
        missedWhilePaused = buffer.length;
        return;
      }
      events = [ev, ...events].slice(0, CAP);
    } catch {
      /* ignore malformed */
    }
  }

  onMount(() => {
    const es = new EventSource('/events/stream');
    es.onopen = () => (conn = 'live');
    es.onerror = () => (conn = conn === 'connecting' ? 'connecting' : 'reconnecting');
    es.addEventListener('feed', (e) => receive((e as MessageEvent).data));
    return () => es.close();
  });

  function setPaused(next: boolean) {
    // Flush what arrived while reading, newest first: nothing is dropped
    // silently, which is the whole reason the pause has a buffer at all.
    if (!next) {
      events = [...buffer, ...events].slice(0, CAP);
      buffer = [];
      missedWhilePaused = 0;
    }
    paused = next;
  }

  function clearFeed() {
    events = [];
    buffer = [];
    missedWhilePaused = 0;
  }

  const TONES = ['all', 'up', 'down', 'neutral'] as const;
  let tone = $state<string>('all');
  let search = $state('');

  function matchesSearch(f: FeedEvent, q: string): boolean {
    if (!q) return true;
    return f.label.toLowerCase().includes(q) || f.payload.toLowerCase().includes(q);
  }

  const rows = $derived.by(() => {
    const q = search.trim().toLowerCase();
    return events.filter((f) => (tone === 'all' || f.tone === tone) && matchesSearch(f, q));
  });

  const upCount = $derived(events.filter((f) => f.tone === 'up').length);
  const downCount = $derived(events.filter((f) => f.tone === 'down').length);

  const connLabel = $derived(
    conn === 'live'
      ? t('admin.events.streaming')
      : conn === 'connecting'
        ? t('admin.events.connecting')
        : t('admin.events.reconnecting')
  );
  const statusLabel = $derived(paused ? t('admin.events.paused') : connLabel);
  const statusTone: StatusTone = $derived(
    paused ? 'neutral' : conn === 'live' ? 'success' : 'warning'
  );
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.events.eyebrow')} description={t('admin.events.description')}>
    {t('admin.events.titlePre')}<em>{t('admin.events.titleEm')}</em>
  </PageHead>

  <PageToolbar>
    {#snippet lead()}
      <SegmentedControl options={TONES} bind:value={tone} label={t('admin.events.toneFilter')} />
    {/snippet}
    {#snippet trail()}
      <div class="toolbar-search">
        <SearchInput bind:value={search} placeholder={t('admin.events.searchPlaceholder')} />
      </div>
      <!-- Switch renders no visible text of its own (its label is the
           accessible name), so the toolbar supplies one. -->
      <span class="switch-field">
        <span class="switch-label">
          {paused && missedWhilePaused
            ? t('admin.events.pauseLabelBuffered', { n: String(missedWhilePaused) })
            : t('admin.events.pauseLabel')}
        </span>
        <Switch
          checked={paused}
          label={paused && missedWhilePaused
            ? t('admin.events.pauseLabelBuffered', { n: String(missedWhilePaused) })
            : t('admin.events.pauseLabel')}
          onchange={setPaused}
        />
      </span>
      <Button variant="ghost" onclick={clearFeed} disabled={events.length === 0}>
        {t('admin.events.clear')}
      </Button>
    {/snippet}
  </PageToolbar>

  <Card>
    <CardHead title={t('admin.events.subject')}>
      {#snippet action()}
        <span class="feed-meta">
          <span class="count up">{t('admin.events.upCount', { n: String(upCount) })}</span>
          <span class="count down">{t('admin.events.downCount', { n: String(downCount) })}</span>
          <span class="conn">
            <StatusDot tone={statusTone} />
            {statusLabel}
          </span>
        </span>
      {/snippet}
    </CardHead>

    <!-- aria-live off on purpose: a lifecycle firehose announced item by item is
         unusable, and the counts above already carry the summary. -->
    <Scroller maxHeight="60vh" aria-live="off" data-lenis-prevent>
      {#if rows.length === 0}
        {#if events.length === 0}
          <EmptyState title={statusLabel} body={t('admin.events.idleBody')} />
        {:else}
          <EmptyState title={t('admin.events.emptyMatch')} />
        {/if}
      {:else}
        <ul class="bb-list" aria-label={t('admin.events.listLabel')}>
          {#each rows as f, i (f.time + f.subject + i)}
            <li class="feed-row" data-cursor="off">
              <StatusDot tone={TONE_DOT[f.tone]} />
              <span class="body">
                <span class="label">{f.label}</span>
                <span class="payload">{f.payload}</span>
              </span>
              <span class="when">{f.time}</span>
            </li>
          {/each}
        </ul>
      {/if}
    </Scroller>
  </Card>
</section>

<style>
  .toolbar-search {
    width: 240px;
  }
  .toolbar-search :global(.search) {
    width: 100%;
  }

  .switch-field {
    display: inline-flex;
    align-items: center;
    gap: 9px;
  }
  .switch-label {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-muted);
    white-space: nowrap;
  }

  .feed-meta {
    display: inline-flex;
    align-items: center;
    gap: 12px;
  }
  .count {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.06em;
  }
  .count.up {
    color: var(--bb-green-glow);
  }
  .count.down {
    color: var(--bb-status-error);
  }
  .conn {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
  }

  .feed-row {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 10px 4px;
    border-bottom: 1px solid var(--rule);
  }
  .feed-row:last-child {
    border-bottom: none;
  }
  .feed-row :global(.dot) {
    margin-top: 5px;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    flex: 1;
  }
  .label {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 13px;
    color: var(--bb-white);
  }
  .payload {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-muted);
    word-break: break-word;
  }
  .when {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-muted);
    white-space: nowrap;
    margin-top: 3px;
  }

  @media (max-width: 760px) {
    .toolbar-search {
      width: 100%;
    }
    .feed-meta {
      gap: 8px;
    }
  }
</style>
