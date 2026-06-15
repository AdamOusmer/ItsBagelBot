<script lang="ts">
  import { onMount } from 'svelte';
  import { Icon } from '@bagel/shared';

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
  <div class="page-head">
    <span class="eyebrow">Ingress status</span>
    <h1>Live <em>events</em></h1>
    <p>Streaming shard up/down and status messages from the ingress fleet.</p>
  </div>

  <div class="card">
    <div class="card-head">
      <h3>Feed</h3>
      <span class="status-pill"><span class="dot"></span> {connected ? 'Streaming' : 'Connecting…'}</span>
    </div>
    <div class="feed">
      {#if events.length === 0}
        <div class="feed-row"><div class="ft"><span style="opacity:.6">Waiting for events…</span></div></div>
      {/if}
      {#each events as f, i (f.time + i)}
        <div class="feed-row">
          <div class="fi {f.tone === 'up' ? 'green' : ''}"><Icon name={icon(f.tone)} size={15} /></div>
          <div class="ft">
            <b>{f.label}</b>
            <span>{f.payload}</span>
          </div>
          <span class="fw">{f.time}</span>
        </div>
      {/each}
    </div>
  </div>
</section>
