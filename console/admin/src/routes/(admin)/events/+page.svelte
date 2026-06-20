<script lang="ts">
  import { onMount } from 'svelte';
  import { Icon, Card, CardHead, PageHead } from '@bagel/shared';

  interface FeedEvent {
    subject: string;
    label: string;
    tone: 'up' | 'down' | 'neutral';
    payload: string;
    time: string;
  }

  let events = $state<FeedEvent[]>([]);
  let connected = $state(false);

  onMount(() => {
    const es = new EventSource('/events/stream');
    es.onopen = () => (connected = true);
    es.onerror = () => (connected = false);
    es.addEventListener('feed', (e) => {
      try {
        const ev = JSON.parse((e as MessageEvent).data) as FeedEvent;
        events = [ev, ...events].slice(0, 200);
      } catch {
        /* ignore malformed */
      }
    });
    return () => es.close();
  });

  function icon(tone: FeedEvent['tone']) {
    return tone === 'up' ? 'check' : tone === 'down' ? 'ban' : 'pulse';
  }
</script>

<section class="screen active">
  <PageHead eyebrow="Ingress status" description="Streaming shard up/down and status messages from the ingress fleet.">Live <em>events</em></PageHead>

  <Card>
    <CardHead title="Feed">
      {#snippet action()}
      <span class="status-pill {connected ? '' : 'dim'}">
        <span class="dot"></span>
        {connected ? 'Streaming' : 'Connecting…'}
      </span>
      {/snippet}
    </CardHead>

    <div class="feed">
      {#if events.length === 0}
        <div class="feed-empty">
          <div class="fi"><Icon name="pulse" size={16} /></div>
          <span>Waiting for events from the ingress fleet…</span>
        </div>
      {/if}
      {#each events as f, i (f.time + i)}
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
  .status-pill.dim {
    background: rgba(255,255,255,.04);
    border-color: var(--glass-border);
    color: var(--bb-muted);
  }
  .status-pill.dim .dot {
    background: var(--bb-muted);
    box-shadow: none;
    animation: none;
  }

  .feed-empty {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 24px 4px;
    color: var(--bb-muted);
    font-family: var(--bb-font-body);
    font-size: 13.5px;
  }
  .feed-empty .fi {
    width: 32px; height: 32px; border-radius: 9px;
    display: flex; align-items: center; justify-content: center;
    background: rgba(255,255,255,.04); border: 1px solid var(--glass-border);
    flex-shrink: 0;
  }
  .feed-empty .fi :global(svg) { stroke: var(--bb-muted); fill: none; width: 16px; height: 16px; stroke-width: 1.6; }

  /* red tone for down events */
  .feed-row .fi.red { background: rgba(176,90,70,.10); border-color: rgba(176,90,70,.28); }
  .feed-row .fi.red :global(svg) { stroke: #cf8a78; }

  /* payload: allow wrapping on mobile */
  .fp {
    display: block;
    font-size: 12.5px;
    color: var(--bb-muted);
    margin-top: 2px;
    word-break: break-word;
  }

  @media (max-width: 760px) {
    .feed-row { flex-wrap: wrap; gap: 8px; }
    .fw { margin-left: 0; }
  }
</style>
