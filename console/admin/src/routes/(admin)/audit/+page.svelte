<script lang="ts">
  import { Icon } from '@bagel/shared';
  import type { AuditEntry } from '$lib/server/rpc';
  let { data } = $props();

  // --- Relative-time helper (seconds/minutes/hours/days ago) ----------
  function relative(iso: string): string {
    const then = new Date(iso).getTime();
    if (Number.isNaN(then)) return '';
    const s = Math.round((Date.now() - then) / 1000);
    if (s < 0) return 'just now';
    if (s < 60) return `${s}s ago`;
    const m = Math.floor(s / 60);
    if (m < 60) return `${m}m ago`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h ago`;
    const d = Math.floor(h / 24);
    return `${d}d ago`;
  }

  function absolute(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
  }

  // --- Client-side filter (actor_login / action / target) -------------
  let filter = $state('');
  const rows = $derived(
    (data.entries as AuditEntry[]).filter((e) => {
      const q = filter.trim().toLowerCase();
      if (!q) return true;
      return (
        e.actor_login.toLowerCase().includes(q) ||
        e.action.toLowerCase().includes(q) ||
        (e.target ?? '').toLowerCase().includes(q)
      );
    })
  );
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Accountability</span>
    <h1>Audit <em>log</em></h1>
    <p>
      Every operator action, attributed. Newest first.{#if data.degraded}
        <em> Live audit data unavailable.</em>{/if}
    </p>
  </div>

  <div class="card" style="padding:18px 6px">
    <div class="card-head" style="padding:0 12px;gap:.6rem">
      <h3>Trail</h3>
      <label class="search search-filter">
        <Icon name="search" size={14} />
        <input type="text" placeholder="Filter by actor, action, or target" autocomplete="off" bind:value={filter} />
      </label>
    </div>

    <div class="table audit-table">
      <div class="thead">
        <span>When</span><span>Actor</span><span>Action</span><span class="col-target">Target</span><span class="col-detail">Detail</span><span>Result</span>
      </div>
      <div class="trows">
        {#if rows.length === 0}
          <div class="trow"><span class="resp" style="grid-column:1/-1;opacity:.6">No matching entries.</span></div>
        {/if}
        {#each rows as e (e.id)}
          <div class="trow">
            <span class="when" title={absolute(e.created_at)}>
              <span class="when-abs">{absolute(e.created_at)}</span>
              <span class="when-rel">{relative(e.created_at)}</span>
            </span>
            <span class="cmd">@{e.actor_login}</span>
            <span class="action">{e.action}</span>
            <span class="resp col-target">{e.target ? e.target : '—'}</span>
            <span class="resp col-detail">{e.detail ? e.detail : '—'}</span>
            <span class="result-cell">
              {#if e.ok}
                <span class="badge mod">ok</span>
              {:else}
                <span class="badge fail" title={e.error ?? 'failed'}>fail</span>
                {#if e.error}<span class="err-note">{e.error}</span>{/if}
              {/if}
            </span>
          </div>
        {/each}
      </div>
    </div>
  </div>
</section>

<style>
  /* card-head with filter input */
  .card-head { align-items: center; }
  .search-filter { margin-left: auto; max-width: 260px; flex: 1; min-width: 0; }

  /* 6-column audit grid: When | Actor | Action | Target | Detail | Result */
  .audit-table .thead,
  .audit-table .trow {
    grid-template-columns: 1.4fr 1.1fr 1.1fr 1fr 1.6fr 0.8fr;
    align-items: start;
  }

  .when { display: flex; flex-direction: column; gap: 2px; }
  .when-abs { font-family: var(--bb-font-mono); font-size: 12.5px; color: var(--bb-white); white-space: nowrap; }
  .when-rel { font-family: var(--bb-font-body); font-size: 11px; color: var(--bb-muted); }

  .action {
    font-family: var(--bb-font-mono); font-size: 12.5px; color: var(--bb-tan-light);
    word-break: break-word;
  }

  .col-target, .col-detail {
    white-space: normal; overflow: visible; text-overflow: clip; word-break: break-word;
  }

  .result-cell { display: flex; flex-direction: column; gap: 4px; align-items: flex-start; }
  .err-note { font-family: var(--bb-font-body); font-size: 11px; color: var(--bb-muted); }

  /* failure badge: red tint mirroring users-page danger styles */
  .badge.fail {
    background: rgba(176, 90, 70, 0.12);
    color: #cf8a78;
    border-color: rgba(176, 90, 70, 0.35);
  }

  /* mobile: hide Detail + Target, keep When/Actor/Action/Result */
  @media (max-width: 760px) {
    .search-filter { max-width: 180px; }
    .audit-table .thead,
    .audit-table .trow {
      grid-template-columns: 1.3fr 1fr 1fr 0.7fr;
      gap: 10px;
    }
    .audit-table .col-target,
    .audit-table .col-detail { display: none; }
  }
</style>
