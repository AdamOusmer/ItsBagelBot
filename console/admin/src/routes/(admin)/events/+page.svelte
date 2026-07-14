<script lang="ts">
  import { onMount } from 'svelte';
  import {
    Icon,
    Card,
    CardHead,
    PageHead,
    PageToolbar,
    SegmentedControl,
    SearchInput,
    EmptyState,
    Button
  } from '@bagel/shared';

  interface FeedEvent {
    subject: string;
    label: string;
    tone: 'up' | 'down' | 'neutral';
    payload: string;
    time: string;
  }

  const CAP = 300;
  let events = $state<FeedEvent[]>([]);
  // Honest connection state: EventSource retries forever, so distinguish "live"
  // from "reconnecting" instead of flashing a generic spinner.
  let conn = $state<'connecting' | 'live' | 'reconnecting'>('connecting');
  let paused = $state(false);
  let missedWhilePaused = $state(0);
  let buffer: FeedEvent[] = [];

  onMount(() => {
    const es = new EventSource('/events/stream');
    es.onopen = () => (conn = 'live');
    es.onerror = () => (conn = conn === 'connecting' ? 'connecting' : 'reconnecting');
    es.addEventListener('feed', (e) => {
      try {
        const ev = JSON.parse((e as MessageEvent).data) as FeedEvent;
        if (paused) {
          buffer = [ev, ...buffer].slice(0, CAP);
          missedWhilePaused = buffer.length;
          return;
        }
        events = [ev, ...events].slice(0, CAP);
      } catch {
        /* ignore malformed */
      }
    });
    return () => es.close();
  });

  function togglePause() {
    if (paused) {
      // Flush what arrived while reading, newest first — nothing dropped silently.
      events = [...buffer, ...events].slice(0, CAP);
      buffer = [];
      missedWhilePaused = 0;
    }
    paused = !paused;
  }

  function clearFeed() {
    events = [];
    buffer = [];
    missedWhilePaused = 0;
  }

  const TONES = ['all', 'up', 'down', 'neutral'] as const;
  let tone = $state<string>('all');
  let search = $state('');

  const rows = $derived(
    events.filter((f) => {
      if (tone !== 'all' && f.tone !== tone) return false;
      const q = search.trim().toLowerCase();
      if (!q) return true;
      return f.label.toLowerCase().includes(q) || f.payload.toLowerCase().includes(q);
    })
  );

  const upCount = $derived(events.filter((f) => f.tone === 'up').length);
  const downCount = $derived(events.filter((f) => f.tone === 'down').length);

  function icon(t: FeedEvent['tone']) {
    return t === 'up' ? 'check' : t === 'down' ? 'ban' : 'pulse';
  }

  const connLabel = $derived(
    conn === 'live' ? 'Streaming' : conn === 'connecting' ? 'Connecting…' : 'Reconnecting…'
  );
</script>

<section class="screen active">
  <PageHead
    eyebrow="Ingress status"
    description="Shard up/down, binding, and status messages streamed straight off the fleet bus."
  >
    Live <em>events</em>
  </PageHead>

  <PageToolbar>
    {#snippet lead()}
      <SegmentedControl options={TONES} bind:value={tone} label="Event tone" />
    {/snippet}
    {#snippet trail()}
      <div class="toolbar-search">
        <SearchInput bind:value={search} placeholder="Filter subject or payload…" />
      </div>
      <Button variant="ghost" onclick={togglePause}>
        {paused ? `Resume${missedWhilePaused ? ` (+${missedWhilePaused})` : ''}` : 'Pause'}
      </Button>
      <Button variant="ghost" onclick={clearFeed} disabled={events.length === 0}>Clear</Button>
    {/snippet}
  </PageToolbar>

  <Card>
    <CardHead title="Feed">
      {#snippet action()}
        <span class="feed-meta">
          <span class="count up">{upCount} up</span>
          <span class="count down">{downCount} down</span>
          <span class="status-pill {conn === 'live' ? '' : 'dim'}">
            <span class="dot"></span>
            {paused ? 'Paused' : connLabel}
          </span>
        </span>
      {/snippet}
    </CardHead>

    <div class="feed" aria-live="off">
      {#if rows.length === 0}
        {#if events.length === 0}
          <EmptyState
            icon="pulse"
            title={conn === 'live' ? 'Quiet so far' : connLabel}
            body="Shard up/down and binding messages appear here the moment the ingress publishes them."
          />
        {:else}
          <EmptyState icon="search" title="No events match the filter" />
        {/if}
      {/if}
      {#each rows as f, i (f.time + f.subject + i)}
        <div class="feed-row">
          <div class="fi {f.tone === 'up' ? 'green' : f.tone === 'down' ? 'red' : ''}">
            <Icon name={icon(f.tone)} size={15} />
          </div>
          <div class="ft">
            <b>{f.label}</b>
            <span class="fp">{f.payload}</span>
          </div>
          <span class="fw">{f.time}</span>
        </div>
      {/each}
    </div>
  </Card>
</section>

<style>
  .toolbar-search { width: 240px; }
  .toolbar-search :global(.search) { width: 100%; }

  .feed-meta { display: inline-flex; align-items: center; gap: 12px; }
  .count { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.06em; }
  .count.up { color: var(--bb-green-glow); }
  .count.down { color: #cf8a78; }

  .status-pill.dim {
    background: rgba(255, 255, 255, 0.04);
    border-color: var(--glass-border);
    color: var(--bb-muted);
  }
  .status-pill.dim .dot { background: var(--bb-muted); box-shadow: none; animation: none; }

  .feed-row .fi.red { background: rgba(176, 90, 70, 0.1); border-color: rgba(176, 90, 70, 0.28); }
  .feed-row .fi.red :global(svg) { stroke: #cf8a78; }

  .fp {
    display: block;
    font-size: 12.5px;
    color: var(--bb-muted);
    margin-top: 2px;
    word-break: break-word;
    font-family: var(--bb-font-mono);
  }

  @media (max-width: 760px) {
    .feed-row { flex-wrap: wrap; gap: 8px; }
    .fw { margin-left: 0; }
    .toolbar-search { width: 100%; }
  }
</style>
