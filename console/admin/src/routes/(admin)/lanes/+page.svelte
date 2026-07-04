<script lang="ts">
  import { enhance } from '$app/forms';
  import { Icon, Card, PageHead, MiniButton } from '@bagel/shared';
  let { data, form } = $props();

  const notice = $derived(form?.notice ?? data.notice);
</script>

<section class="screen active">
  <PageHead eyebrow="NATS JetStream" description="Durable and ephemeral consumers across the fleet streams.">Lane <em>telemetry</em></PageHead>

  {#if data.degraded}
    <div class="card degraded-notice">
      <div class="card-head">
        <div class="notice-icon"><Icon name="lanes" size={16} /></div>
        <h3>Lane data unavailable</h3>
      </div>
      <p class="notice-body">
        Lane telemetry reads directly from the JetStream management API, which the console
        RPC client does not speak. A dedicated admin RPC subject must be added before this
        view can show live data.
      </p>
      <p class="notice-sub">No actions (alias, make-permanent, delete) are available either.</p>
    </div>
  {:else if notice}
    <div class="card notice-card">
      <div class="card-head"><h3>Notice</h3></div>
      <p class="notice-body">{notice}</p>
    </div>
  {/if}

  {#if !data.degraded}
    <Card style="padding:18px 6px">
      <div class="card-head" style="padding:0 12px">
        <h3>Consumers</h3>
        <span class="more">{data.lanes.length} lane{data.lanes.length !== 1 ? 's' : ''}</span>
      </div>
      <div class="table">
        <div class="thead">
          <span>Lane</span><span>Subject</span><span>Pending</span><span>In-flight</span><span>Rate</span><span></span>
        </div>
        <div class="trows">
          {#if data.lanes.length === 0}
            <div class="trow">
              <span class="resp empty-cell">No lane telemetry available.</span>
            </div>
          {/if}
          {#each data.lanes as l (l.stream + l.consumer)}
            <div class="trow {l.orphan ? 'off' : ''}" style={l.orphan ? 'opacity:.65' : ''}>
              <span class="cmd lane-name" title={l.consumer}>
                {l.display}
                {#if l.ephemeral}<span class="lane-tag">ephemeral</span>{/if}
                {#if l.orphan}<span class="lane-tag warn">orphan</span>{/if}
              </span>
              <span class="resp">{l.subject}</span>
              <span class="cd">{l.pending}</span>
              <span class="cd">{l.inFlight}</span>
              <span class="uses">{l.rate}</span>
              <span class="row-act">
                <form method="POST" action="?/alias" use:enhance>
                  <input type="hidden" name="stream" value={l.stream} />
                  <input type="hidden" name="consumer" value={l.consumer} />
                  <MiniButton icon="edit" aria-label="Clear alias" title="Clear display alias" />
                </form>
                {#if l.ephemeral}
                  <form method="POST" action="?/durable" use:enhance>
                    <input type="hidden" name="stream" value={l.stream} />
                    <input type="hidden" name="consumer" value={l.consumer} />
                    <MiniButton icon="lock" aria-label="Make permanent" title="Make permanent (pin as durable)" />
                  </form>
                {/if}
                {#if l.orphan}
                  <form method="POST" action="?/delete" use:enhance>
                    <input type="hidden" name="stream" value={l.stream} />
                    <input type="hidden" name="consumer" value={l.consumer} />
                    <MiniButton icon="trash" aria-label="Delete orphan lane" title="Delete orphan lane" />
                  </form>
                {/if}
              </span>
            </div>
          {/each}
        </div>
      </div>
    </Card>
  {/if}
</section>

<style>
  .degraded-notice {
    border-color: rgba(201,168,124,.35) !important;
    background: rgba(201,168,124,.05) !important;
  }
  .degraded-notice .card-head {
    margin-bottom: 12px;
  }
  .notice-icon {
    width: 30px; height: 30px; border-radius: 8px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    background: rgba(201,168,124,.12); border: 1px solid rgba(201,168,124,.3);
    margin-right: 10px;
  }
  .notice-icon :global(svg) { stroke: var(--bb-tan-light); fill: none; width: 16px; height: 16px; stroke-width: 1.7; }

  .notice-body {
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    color: var(--bb-muted);
    line-height: 1.55;
    margin: 0 0 8px;
  }
  .notice-sub {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    opacity: .7;
    margin: 0;
  }

  .notice-card { border-color: rgba(201,168,124,.28) !important; }

  .lane-name { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .lane-tag {
    font-family: var(--bb-font-mono);
    font-size: 9px;
    letter-spacing: .06em;
    text-transform: uppercase;
    padding: 2px 7px;
    border-radius: 8px;
    background: rgba(255,255,255,.04);
    border: 1px solid var(--glass-border);
    color: var(--bb-muted);
  }
  .lane-tag.warn {
    background: rgba(201,168,124,.08);
    border-color: rgba(201,168,124,.28);
    color: var(--bb-tan-light);
  }

  .empty-cell { grid-column: 1/-1; opacity: .6; }

  @media (max-width: 760px) {
    .degraded-notice { padding: 16px; }
  }
</style>
