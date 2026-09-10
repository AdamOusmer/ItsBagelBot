<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The operator audit trail, on the shared deck + inspector. Manager-only; the
  // gate is in +page.server.ts and /audit/data re-asks it, so a page that
  // rendered would still get nothing without the role.
  //
  // Paging moved from prev/next to load-more. The trail is read backwards from
  // "what just happened", and a numbered pager makes that a sequence of
  // full-list replacements: page 2 threw page 1 away, so an entry seen a moment
  // ago could not be scrolled back to. Appending keeps everything read so far on
  // screen.
  //
  // Search stays server-side (it covers the whole trail, not the loaded page);
  // the kind filter is client-side over what has been loaded, and says so.
  import { onMount } from 'svelte';
  import PageHead from '@bagel/kit/components/PageHead.svelte';
  import PageToolbar from '@bagel/kit/components/PageToolbar.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import DeckList from '@bagel/kit/components/DeckList.svelte';
  import InspectorSurface from '@bagel/kit/components/InspectorSurface.svelte';
  import AlertBanner from '@bagel/kit/components/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AuditEntry } from '$lib/server/services';
  import AuditRow from '$lib/components/audit/AuditRow.svelte';
  import AuditDetail from '$lib/components/audit/AuditDetail.svelte';
  import { auditCsv } from '$lib/components/audit/csv';
  import { downloadCsv } from '$lib/csv';
  import { AUDIT_KINDS, KIND_LABEL, inKind, type AuditKind } from '$lib/components/audit/audit-kinds';

  let { data } = $props();

  const { t } = getI18n();

  // ── Client-fetched pages from /audit/data ──────────────────────────────────
  // null = the first page is still in flight; [] = it landed empty.
  let entries = $state<AuditEntry[] | null>(null);
  let page = $state(1);
  let hasMore = $state(false);
  let loadingMore = $state(false);
  let fetchError = $state('');
  // svelte-ignore state_referenced_locally
  let search = $state(data.search);

  type AuditWire = {
    entries?: AuditEntry[];
    page?: number;
    has_more?: boolean;
    error?: string;
  };

  // A generation counter, not an AbortController: the request that lost the race
  // has usually already resolved, and the bug this prevents is a slow FIRST page
  // overwriting a fast second search, which cancelling cannot help with.
  let seq = 0;

  async function fetchPage(wanted: number, q: string, append: boolean) {
    const mine = ++seq;
    if (append) loadingMore = true;
    else entries = null;
    fetchError = '';
    try {
      const params = new URLSearchParams();
      if (wanted > 1) params.set('page', String(wanted));
      if (q) params.set('q', q);
      const res = await fetch(`/audit/data?${params}`);
      if (!res.ok) throw new Error(`audit fetch failed (${res.status})`);
      const body = (await res.json()) as AuditWire;
      if (mine !== seq) return; // a newer request superseded this one
      if (body.error) fetchError = body.error;
      apply(body, wanted, append);
    } catch (e) {
      if (mine !== seq) return;
      fetchError = (e as Error).message;
      entries = entries ?? [];
    } finally {
      if (mine === seq) loadingMore = false;
    }
  }

  function apply(body: AuditWire, wanted: number, append: boolean) {
    const next = body.entries ?? [];
    entries = append ? [...(entries ?? []), ...next] : next;
    page = body.page ?? wanted;
    hasMore = Boolean(body.has_more);
  }

  onMount(() => {
    fetchPage(1, search.trim(), false);
  });

  function submitSearch(q: string) {
    search = q;
    closeInspector();
    fetchPage(1, q.trim(), false);
  }

  // ── Kind filter (client-side, over what is loaded) ─────────────────────────
  const kindLabels = $derived(AUDIT_KINDS.map((k) => t(KIND_LABEL[k])));
  let kind = $state<AuditKind>('all');
  const kindLabel = $derived(kindLabels[AUDIT_KINDS.indexOf(kind)]);

  function pickKind(label: string) {
    const next = AUDIT_KINDS[kindLabels.indexOf(label)];
    if (next) kind = next;
  }

  const loaded = $derived(entries ?? []);
  const rows = $derived(loaded.filter((e) => inKind(e, kind)));
  const failCount = $derived(loaded.filter((e) => !e.ok).length);

  // ── Selection ──────────────────────────────────────────────────────────────
  let selectedId = $state<number | null>(null);
  const selected = $derived(loaded.find((e) => e.id === selectedId) ?? null);

  function closeInspector() {
    selectedId = null;
  }

  function openEntry(entry: AuditEntry) {
    selectedId = selectedId === entry.id ? null : entry.id;
  }

  // Exports what is ON SCREEN, kind filter included -- not everything loaded.
  // An export that quietly carries rows the operator has filtered out is how a
  // "failed actions only" spreadsheet ends up with successes in it.
  function exportCsv() {
    const suffix = search.trim() ? `-${search.trim()}` : '';
    downloadCsv(`audit-${kind}${suffix}.csv`, auditCsv(rows));
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.audit.eyebrow')} description={t('admin.audit.description')}>
    {t('admin.audit.titlePre')}<em>{t('admin.audit.titleEm')}</em>
  </PageHead>

  <PageToolbar>
    {#snippet lead()}
      {#if entries}
        <span class="stats">
          {t('admin.audit.stats', {
            loaded: String(loaded.length),
            failed: String(failCount)
          })}
        </span>
      {/if}
    {/snippet}
    {#snippet trail()}
      <div class="toolbar-search">
        <SearchInput
          bind:value={search}
          placeholder={t('admin.audit.searchPlaceholder')}
          debounceMs={350}
          oninput={submitSearch}
        />
      </div>
      <Button variant="ghost" onclick={exportCsv} disabled={rows.length === 0}>
        {t('admin.audit.exportCsv')}
      </Button>
    {/snippet}
  </PageToolbar>

  <div class="filters">
    <SegmentedControl
      options={kindLabels}
      label={t('admin.audit.kindFilter')}
      bind:value={() => kindLabel, pickKind}
    />
  </div>

  {#if fetchError}
    <AlertBanner>{t('admin.audit.unreachable', { error: fetchError })}</AlertBanner>
  {/if}

  <div class="deck" class:inspecting={selected !== null}>
    <DeckList>
      {#if entries === null}
        <SkeletonStack rows={6} height="52px" />
      {:else if rows.length}
        <ul class="bb-list" aria-label={t('admin.audit.listLabel')}>
          {#each rows as entry (entry.id)}
            <li>
              <AuditRow
                {entry}
                selected={selectedId === entry.id}
                controls="audit-inspector"
                onselect={() => openEntry(entry)}
              />
            </li>
          {/each}
        </ul>
      {:else if loaded.length}
        <EmptyState title={t('admin.audit.emptyKind')} body={t('admin.audit.emptyKindBody')} />
      {:else if search.trim()}
        <EmptyState title={t('admin.audit.emptyMatch')} body={t('admin.audit.emptyMatchBody')} />
      {:else}
        <EmptyState title={t('admin.audit.empty')} />
      {/if}

      {#if entries && hasMore}
        <div class="more">
          <Button
            variant="ghost"
            loading={loadingMore}
            onclick={() => fetchPage(page + 1, search.trim(), true)}
          >
            {t('admin.audit.loadMore')}
          </Button>
        </div>
      {:else if entries && page >= data.maxPages}
        <p class="cap">{t('admin.audit.pageCap', { max: String(data.maxPages) })}</p>
      {/if}
    </DeckList>

    {#if selected}
      <InspectorSurface
        open
        title={t('admin.audit.inspectorTitle', { action: selected.action })}
        controls="audit-inspector"
        closeLabel={t('admin.close')}
        onClose={closeInspector}
      >
        <AuditDetail entry={selected} />
      </InspectorSurface>
    {/if}
  </div>
</section>

<style>
  .stats {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
  }

  .toolbar-search {
    width: 260px;
  }
  .toolbar-search :global(.bb-input) {
    width: 100%;
  }

  .filters {
    margin: 0 0 14px;
  }

  .deck {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  @media (min-width: 1080px) {
    .deck.inspecting {
      grid-template-columns: minmax(0, 1fr) 380px;
    }
  }

  .more {
    display: flex;
    justify-content: center;
    padding: 14px;
  }
  .cap {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    text-align: center;
    margin: 0;
    padding: 14px;
  }

  @media (max-width: 680px) {
    .toolbar-search {
      width: 100%;
    }
  }
</style>
