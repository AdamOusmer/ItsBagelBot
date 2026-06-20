<script lang="ts">
  import { Icon, Card, PageHead, Button } from '@bagel/shared';
  import type { AuditEntry } from '$lib/server/rpc';
  let { data } = $props();

  const search = $derived(String(data.search ?? ''));
  const page = $derived(Number(data.page ?? 1));
  let entries = $state<AuditEntry[]>([]);
  // svelte-ignore state_referenced_locally
  let pageSize = $state(Number(data.pageSize ?? 15));
  // svelte-ignore state_referenced_locally
  let maxPages = $state(Number(data.maxPages ?? 25));
  let hasMore = $state(false);
  let degraded = $state(false);
  let loading = $state(false);
  let reqId = 0;
  const showingStart = $derived(entries.length === 0 ? 0 : (page - 1) * pageSize + 1);
  const showingEnd = $derived(showingStart === 0 ? 0 : showingStart + entries.length - 1);

  function auditHref(pageNo: number, q: string): string {
    const params = new URLSearchParams();
    const clean = q.trim();
    if (clean) params.set('q', clean);
    if (pageNo > 1) params.set('page', String(pageNo));
    const query = params.toString();
    return query ? `/audit?${query}` : '/audit';
  }

  async function loadAudit(pageNo: number, q: string) {
    const req = ++reqId;
    loading = true;
    degraded = false;
    entries = [];
    const params = new URLSearchParams();
    if (q.trim()) params.set('q', q.trim());
    if (pageNo > 1) params.set('page', String(pageNo));
    const query = params.toString();
    try {
      const res = await fetch(query ? `/audit/data?${query}` : '/audit/data');
      if (!res.ok) throw new Error(`request failed (${res.status})`);
      const body = (await res.json()) as {
        entries?: AuditEntry[];
        page_size?: number;
        max_pages?: number;
        has_more?: boolean;
        error?: string;
      };
      if (req !== reqId) return;
      entries = body.entries ?? [];
      pageSize = Number(body.page_size ?? pageSize);
      maxPages = Number(body.max_pages ?? maxPages);
      hasMore = Boolean(body.has_more);
      degraded = Boolean(body.error);
    } catch {
      if (req !== reqId) return;
      entries = [];
      hasMore = false;
      degraded = true;
    } finally {
      if (req === reqId) loading = false;
    }
  }

  $effect(() => {
    loadAudit(page, search);
  });

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

</script>

<section class="screen active">
  <PageHead eyebrow="Accountability" description="Every operator action, attributed. Newest first.">Audit <em>log</em></PageHead>
  {#if degraded}<p class="degraded-note"><em>Live audit data unavailable.</em></p>{/if}

  <Card style="padding:18px 6px">
    <div class="card-head" style="padding:0 12px;gap:.6rem">
      <h3>Trail</h3>
      <form method="GET" action="/audit" class="audit-controls">
        <label class="search search-filter">
          <Icon name="search" size={14} />
          <input name="q" type="text" placeholder="Search actor, action, target, or detail" autocomplete="off" value={search} />
        </label>
        <Button variant="primary" class="search-submit" type="submit" icon="search">
          <span>Search</span>
        </Button>
        {#if search}
          <a class="btn ghost clear-search" href="/audit">Clear</a>
        {/if}
      </form>
    </div>

    <div class="table audit-table">
      <div class="thead">
        <span>When</span><span>Actor</span><span>Action</span><span class="col-target">Target</span><span class="col-detail">Detail</span><span>Result</span>
      </div>
      <div class="trows">
        {#if loading}
          <div class="trow"><span class="resp" style="grid-column:1/-1;opacity:.6">Loading audit entries...</span></div>
        {:else if entries.length === 0}
          <div class="trow"><span class="resp" style="grid-column:1/-1;opacity:.6">No matching entries.</span></div>
        {/if}
        {#each entries as e (e.id)}
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

    <div class="audit-foot">
      <span class="page-state">
        {#if showingStart === 0}
          Page {page}
        {:else}
          {showingStart}-{showingEnd} · page {page}
        {/if}
        <span class="muted">of {maxPages} max</span>
      </span>
      <div class="pager">
        {#if page > 1}
          <a class="pager-link" href={auditHref(page - 1, search)}>Previous</a>
        {:else}
          <span class="pager-link disabled" aria-disabled="true">Previous</span>
        {/if}
        {#if hasMore && page < maxPages}
          <a class="pager-link" href={auditHref(page + 1, search)}>Next</a>
        {:else}
          <span class="pager-link disabled" aria-disabled="true">Next</span>
        {/if}
      </div>
    </div>
  </Card>
</section>

<style>
  .degraded-note { font-family: var(--bb-font-body); font-size: 15px; line-height: 1.55; color: var(--bb-muted); margin: -16px 0 20px; max-width: 560px; }
  .degraded-note em { font-style: italic; }

  /* card-head with filter input */
  .card-head { align-items: center; }
  .audit-controls {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    flex: 1;
    min-width: 0;
    margin-left: auto;
  }
  .search-filter { max-width: 340px; flex: 1; min-width: 220px; width: auto; }
  :global(.search-submit) { padding: 10px 14px; }
  .clear-search { padding: 10px 14px; text-decoration: none; }

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

  .audit-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 14px 20px 2px;
    margin-top: 12px;
    border-top: 1px solid var(--glass-border);
  }
  .page-state {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--bb-tan-light);
  }
  .page-state .muted { color: var(--bb-muted); }
  .pager { display: flex; align-items: center; gap: 8px; }
  .pager-link {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 86px;
    padding: 9px 13px;
    border-radius: var(--bb-radius-pill);
    border: 1px solid var(--glass-border);
    background: rgba(255,255,255,0.03);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    text-decoration: none;
    transition: all var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .pager-link:hover {
    background: rgba(201,168,124,0.08);
    border-color: var(--bb-border-strong);
    color: var(--bb-tan-pale);
  }
  .pager-link.disabled {
    opacity: 0.42;
    pointer-events: none;
  }

  /* failure badge: red tint mirroring users-page danger styles */
  .badge.fail {
    background: rgba(176, 90, 70, 0.12);
    color: #cf8a78;
    border-color: rgba(176, 90, 70, 0.35);
  }

  /* mobile: hide Detail + Target, keep When/Actor/Action/Result */
  @media (max-width: 760px) {
    .card-head { align-items: stretch; flex-direction: column; }
    .audit-controls { width: 100%; margin-left: 0; justify-content: flex-start; flex-wrap: wrap; }
    .search-filter { max-width: none; min-width: 100%; }
    :global(.search-submit) { flex: 1; justify-content: center; }
    .clear-search { flex: 1; justify-content: center; }
    .audit-table .thead,
    .audit-table .trow {
      grid-template-columns: 1.3fr 1fr 1fr 0.7fr;
      gap: 10px;
    }
    .audit-table .col-target,
    .audit-table .col-detail { display: none; }
    .audit-foot { align-items: stretch; flex-direction: column; padding-inline: 14px; }
    .pager { display: grid; grid-template-columns: 1fr 1fr; width: 100%; }
    .pager-link { min-width: 0; }
  }
</style>
