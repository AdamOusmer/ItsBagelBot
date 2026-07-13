<script lang="ts">
  import { onMount } from 'svelte';
  import {
    PageHead,
    PageToolbar,
    SearchInput,
    SegmentedControl,
    DeckList,
    EmptyState,
    Skeleton,
    AlertBanner
  } from '@bagel/shared';
  import type { AuditEntry } from '$lib/server/services';

  let { data } = $props();

  // Client-fetched pages from /audit/data so search + paging never re-run SSR.
  let entries = $state<AuditEntry[] | null>(null);
  // svelte-ignore state_referenced_locally
  let page = $state(data.page);
  let hasMore = $state(false);
  let fetchError = $state('');
  // svelte-ignore state_referenced_locally
  let search = $state(data.search);

  let seq = 0;
  async function fetchPage(p: number, q: string) {
    const mySeq = ++seq;
    entries = null;
    fetchError = '';
    try {
      const params = new URLSearchParams();
      if (p > 1) params.set('page', String(p));
      if (q) params.set('q', q);
      const res = await fetch(`/audit/data?${params}`);
      if (!res.ok) throw new Error(`audit fetch failed (${res.status})`);
      const body = (await res.json()) as {
        entries?: AuditEntry[];
        page?: number;
        has_more?: boolean;
        error?: string;
      };
      if (mySeq !== seq) return; // a newer request superseded this one
      if (body.error) fetchError = body.error;
      entries = body.entries ?? [];
      page = body.page ?? p;
      hasMore = Boolean(body.has_more);
    } catch (e) {
      if (mySeq !== seq) return;
      fetchError = (e as Error).message;
      entries = [];
    }
  }

  onMount(() => {
    fetchPage(page, search);
  });

  function submitSearch(q: string) {
    search = q;
    fetchPage(1, q.trim());
  }

  // Quick outcome filter over the loaded page (server search stays the source
  // for text; this just narrows what's on screen).
  const OUTCOMES = ['all', 'ok', 'failed'] as const;
  let outcome = $state<string>('all');
  const rows = $derived(
    (entries ?? []).filter((e) => (outcome === 'all' ? true : outcome === 'ok' ? e.ok : !e.ok))
  );
  const failCount = $derived((entries ?? []).filter((e) => !e.ok).length);

  function ago(iso: string): string {
    const mins = Math.max(Math.round((Date.now() - new Date(iso).getTime()) / 60e3), 0);
    if (mins < 1) return 'now';
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.round(mins / 60);
    if (hours < 48) return `${hours}h ago`;
    return `${Math.round(hours / 24)}d ago`;
  }
</script>

<section class="screen active">
  <PageHead eyebrow="Access control" description="Every operator action, who ran it, and whether it worked.">
    Audit <em>trail</em>
  </PageHead>

  <PageToolbar>
    {#snippet lead()}
      <SegmentedControl options={OUTCOMES} bind:value={outcome} label="Outcome" />
      {#if entries && failCount > 0}
        <span class="fail-note">{failCount} failed on this page</span>
      {/if}
    {/snippet}
    {#snippet trail()}
      <div class="toolbar-search">
        <SearchInput
          bind:value={search}
          placeholder="Actor, action, target…"
          debounceMs={350}
          oninput={submitSearch}
        />
      </div>
    {/snippet}
  </PageToolbar>

  {#if fetchError}
    <AlertBanner>Audit log unreachable: {fetchError}</AlertBanner>
  {/if}

  <DeckList>
    {#if entries === null}
      <div class="row-skeletons">
        {#each [0, 1, 2, 3, 4, 5] as i (i)}<Skeleton variant="block" height="48px" />{/each}
      </div>
    {:else if rows.length}
      <ul class="list" aria-label="Audit entries">
        {#each rows as e (e.id)}
          <li class="audit-row">
            <span class="adot {e.ok ? '' : 'err'}"></span>
            <div class="abody">
              <span class="aline">
                <b>@{e.actor_login}</b>
                <span class="aaction">{e.action}</span>
                {#if e.target}<span class="atarget">→ {e.target}</span>{/if}
              </span>
              {#if e.detail}<span class="adetail">{e.detail}</span>{/if}
              {#if !e.ok && e.error}<span class="adetail err">{e.error}</span>{/if}
            </div>
            <span class="awhen">{ago(e.created_at)}</span>
          </li>
        {/each}
      </ul>
    {:else if (entries ?? []).length > 0}
      <EmptyState icon="search" title="No entries match the outcome filter" />
    {:else if search}
      <EmptyState icon="search" title="No entries match" body="Search covers actor, action, target, detail, and error text." />
    {:else}
      <EmptyState icon="audit" title="No actions recorded yet" />
    {/if}

    {#if entries && (page > 1 || hasMore)}
      <div class="pager">
        <button class="btn ghost" disabled={page <= 1} onclick={() => fetchPage(page - 1, search.trim())}>
          ← Prev
        </button>
        <span class="pager-label">page {page}</span>
        <button class="btn ghost" disabled={!hasMore} onclick={() => fetchPage(page + 1, search.trim())}>
          Next →
        </button>
      </div>
    {/if}
  </DeckList>
</section>

<style>
  .toolbar-search { width: 260px; }
  .toolbar-search :global(.search) { width: 100%; }
  .fail-note { font-family: var(--bb-font-mono); font-size: 11px; color: #cf8a78; margin-left: 10px; }

  .row-skeletons { display: flex; flex-direction: column; gap: 8px; padding: 12px; }

  .list { list-style: none; margin: 0; padding: 0; }
  .audit-row {
    display: flex; align-items: flex-start; gap: 12px;
    padding: 12px 14px; border-bottom: 1px solid var(--rule);
  }
  .audit-row:last-child { border-bottom: none; }

  .adot { width: 8px; height: 8px; border-radius: 50%; background: var(--bb-green-glow); box-shadow: 0 0 8px var(--bb-green-glow); margin-top: 5px; flex: none; }
  .adot.err { background: #cf8a78; box-shadow: 0 0 8px rgba(176, 90, 70, 0.6); }

  .abody { display: flex; flex-direction: column; gap: 3px; min-width: 0; flex: 1; }
  .aline { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; }
  .aline b { font-family: var(--bb-font-body); font-weight: 600; font-size: 13px; color: var(--bb-white); }
  .aaction { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-tan-light); }
  .atarget { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-muted); }
  .adetail { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-muted); word-break: break-word; }
  .adetail.err { color: #cf8a78; }
  .awhen { font-family: var(--bb-font-mono); font-size: 10.5px; color: var(--bb-muted); white-space: nowrap; margin-top: 3px; }

  .pager { display: flex; align-items: center; justify-content: center; gap: 14px; padding: 14px; }
  .pager-label { font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-muted); }

  @media (max-width: 680px) {
    .toolbar-search { width: 100%; }
  }
</style>
