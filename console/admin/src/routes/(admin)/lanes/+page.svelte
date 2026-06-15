<script lang="ts">
  import { enhance } from '$app/forms';
  import { Icon, Button } from '@bagel/shared';
  let { data, form } = $props();

  const notice = $derived(form?.notice ?? data.notice);
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">NATS JetStream</span>
    <h1>Lane <em>telemetry</em></h1>
    <p>Durable and ephemeral consumers across the fleet streams.</p>
  </div>

  {#if notice}
    <div class="card" style="border-color:var(--glass-border)">
      <div class="card-head"><h3>Lane management unavailable</h3></div>
      <p class="text-muted" style="font-size:.85rem">{notice}</p>
    </div>
  {/if}

  <div class="card" style="padding:18px 6px">
    <div class="table">
      <div class="thead">
        <span>Lane</span><span>Subject</span><span>Pending</span><span>In-flight</span><span>Rate</span><span></span>
      </div>
      <div class="trows">
        {#if data.lanes.length === 0}
          <div class="trow"><span class="resp" style="grid-column:1/-1;opacity:.6">No lane telemetry available.</span></div>
        {/if}
        {#each data.lanes as l (l.stream + l.consumer)}
          <div class="trow {l.orphan ? 'off' : ''}" style={l.orphan ? 'opacity:.65' : ''}>
            <span class="cmd" title={l.consumer}>{l.display}</span>
            <span class="resp">{l.subject}</span>
            <span class="cd">{l.pending}</span>
            <span class="cd">{l.inFlight}</span>
            <span class="uses">{l.rate}</span>
            <span class="row-act">
              <form method="POST" action="?/alias" use:enhance>
                <input type="hidden" name="stream" value={l.stream} />
                <input type="hidden" name="consumer" value={l.consumer} />
                <button class="mini" aria-label="Rename"><Icon name="edit" size={15} /></button>
              </form>
              {#if l.ephemeral}
                <form method="POST" action="?/durable" use:enhance>
                  <input type="hidden" name="stream" value={l.stream} />
                  <input type="hidden" name="consumer" value={l.consumer} />
                  <button class="mini" aria-label="Make permanent"><Icon name="lock" size={15} /></button>
                </form>
              {/if}
              {#if l.orphan}
                <form method="POST" action="?/delete" use:enhance>
                  <input type="hidden" name="stream" value={l.stream} />
                  <input type="hidden" name="consumer" value={l.consumer} />
                  <button class="mini" aria-label="Delete"><Icon name="trash" size={15} /></button>
                </form>
              {/if}
            </span>
          </div>
        {/each}
      </div>
    </div>
  </div>
</section>
