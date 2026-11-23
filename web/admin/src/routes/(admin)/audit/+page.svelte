<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AuditEntry } from '#lib/server/services';
  import type { AuditPageResult } from '#lib/components/audit/audit-page.server';
  import AuditRow from '#lib/components/audit/AuditRow.svelte';
  import AuditDetail from '#lib/components/audit/AuditDetail.svelte';
  import { auditCsv } from '#lib/components/audit/csv';
  import { applyStreamed } from '#lib/streamed-load';
  import { downloadCsv } from '@bagel/ui/lib/csv';
  import { AUDIT_KINDS, KIND_LABEL, auditKind, auditKindCounts, type AuditKind } from '#lib/components/audit/audit-kinds';

  let { data } = $props();

  const { t } = getI18n();
  const FILTERS = ['all', ...AUDIT_KINDS] as const;

  let search = $state(untrack(() => data.search));
  let kinds = $state<AuditKind[]>([]);
  let selectedId = $state<number | null>(null);

  $effect(() => {
    search = data.search;
    closeInspector();
  });

  type AuditResult = Pick<AuditPageResult, 'entries' | 'page' | 'has_more' | 'error'>;
  type SearchRead = { base: Promise<AuditPageResult>; term: string; result: Promise<AuditResult> };
  type Trail = {
    firstPage: Promise<AuditResult>;
    entries: AuditEntry[];
    page: number;
    hasMore: boolean;
    busy: boolean;
    error: string;
  };

  let localSearch = $state.raw<SearchRead>();
  let trail = $state.raw<Trail>();
  let resolved = $state.raw<{ source: Promise<AuditResult>; value: AuditResult }>();
  const searchRead = $derived(localSearch?.base === data.firstPage ? localSearch : undefined);
  const firstPage = $derived(searchRead?.result ?? data.firstPage);
  const first = $derived(resolved?.source === firstPage ? resolved.value : undefined);
  const term = $derived(searchRead?.term ?? data.search);
  const more = $derived(trail?.firstPage === firstPage ? trail : undefined);
  const loaded = $derived(first ? [...first.entries, ...(more?.entries ?? [])] : []);
  const rows = $derived(kinds.length ? loaded.filter((entry) => kinds.includes(auditKind(entry.action))) : loaded);
  const counts = $derived(auditKindCounts(loaded));
  const selected = $derived(rows.find((entry) => entry.id === selectedId));
  const page = $derived(more?.page ?? first?.page);
  const hasMore = $derived(more?.hasMore ?? first?.has_more);
  const issue = $derived(more?.error || first?.error);
  const atCap = $derived(page === data.maxPages && !issue);

  $effect(() => {
    const source = firstPage;
    applyStreamed(source, (value) => { resolved = { source, value }; });
  });

  function loadSearch(value: string) {
    const q = value.trim();
    const searchPage = searchRead ? 1 : data.page;
    if (q === term && searchPage === 1) return;
    closeInspector();
    localSearch = { base: data.firstPage, term: q, result: fetchPage(1, q) };
  }

  async function fetchPage(page: number, q: string): Promise<AuditResult> {
    try {
      const query = new URLSearchParams({ page: String(page), q });
      const response = await fetch('/audit/data?' + query);
      if (!response.ok) throw new Error(String(response.status));
      return await response.json();
    } catch (error) {
      return {
        entries: [],
        page,
        has_more: false,
        error: error instanceof Error ? error.message : String(error)
      };
    }
  }

  async function loadMore(page: number, loaded: AuditEntry[]) {
    if (more?.busy) return;
    const current = more ?? {
      firstPage,
      entries: [],
      page,
      hasMore: true
    };
    trail = { ...current, busy: true, error: '' };
    const next = await fetchPage(page + 1, term);
    if (current.firstPage !== firstPage) return;
    const seen = new Set(loaded.map((entry) => entry.id));
    const settled = next.error ? current : {
      firstPage,
      entries: [...current.entries, ...next.entries.filter((entry) => !seen.has(entry.id))],
      page: next.page,
      hasMore: next.has_more
    };
    trail = { ...settled, busy: false, error: next.error ?? '' };
  }

  function toggleKind(kind: AuditKind | 'all') {
    closeInspector();
    if (kind === 'all') {
      kinds = [];
      return;
    }
    kinds = kinds.includes(kind) ? kinds.filter((active) => active !== kind) : [...kinds, kind];
  }

  function closeInspector() {
    selectedId = null;
  }

  function openEntry(entry: AuditEntry) {
    selectedId = selectedId === entry.id ? null : entry.id;
  }

  function exportCsv(rows: AuditEntry[]) {
    const suffix = term ? '-' + term : '';
    const filter = kinds.length ? kinds.join('-') : 'all';
    downloadCsv('audit-' + filter + suffix + '.csv', auditCsv(rows));
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.audit.eyebrow')} description={t('admin.audit.description')}>
    {t('admin.audit.titlePre')}<em>{t('admin.audit.titleEm')}</em>
  </PageHead>

  <Stack gap={3}>
    <Stack gap={2}>
      <Cluster justify="between" nowrap>
        <Text as="span" id="audit-kinds" size="xs" tone="muted" style="inline-size: min(10rem, 45%);">
          <bb-fit><span data-fit>{t('admin.audit.kindFilter')}</span></bb-fit>
        </Text>
        <div class="stats">
          <Text as="div" size="xs" tone="muted" mono>
            {#if first}
              <bb-fit><span data-fit>{t('admin.audit.stats', {
                loaded: String(loaded.length),
                failed: String(loaded.filter((entry) => !entry.ok).length)
              })}</span></bb-fit>
            {:else}
              <Skeleton width="min(19ch, 100%)" />
            {/if}
          </Text>
        </div>
      </Cluster>
      <Grid min="8rem" gap={2} role="group" aria-labelledby="audit-kinds">
        {#each FILTERS as kind (kind)}
          {@const count = kind === 'all' ? loaded.length : counts.get(kind) ?? 0}
          <Chip
            pressed={kind === 'all' ? kinds.length === 0 : kinds.includes(kind)}
            onclick={() => toggleKind(kind)}
            disabled={!first}
            style="min-inline-size: 0; justify-content: space-between;"
          >
            <span class="kind-label"><bb-fit><span data-fit>{t(KIND_LABEL[kind])}</span></bb-fit></span>
            <span class="kind-count"><bb-fit>{first ? count : ''}</bb-fit></span>
          </Chip>
        {/each}
      </Grid>
    </Stack>
    <PageToolbar>
      {#snippet leading()}
        <div style="inline-size: 15rem; max-inline-size: 100%;">
          <SearchInput
            tall
            fill
            bind:value={search}
            placeholder={t('admin.audit.searchPlaceholder')}
            aria-label={t('admin.audit.searchPlaceholder')}
            debounceMs={350}
            onValueChange={loadSearch}
          />
        </div>
      {/snippet}
      {#snippet trailing()}
        <Button variant="secondary" onclick={() => exportCsv(rows)} disabled={rows.length === 0} style="--btn-w: 9rem; --btn-justify: center;">
          <bb-fit><span data-fit>{t('admin.audit.exportCsv')}</span></bb-fit>
        </Button>
      {/snippet}
    </PageToolbar>
  </Stack>

  {#if issue}
    <AlertBanner>{t('admin.audit.unreachable', { error: issue })}</AlertBanner>
  {/if}

  <DeckLayout inspecting={selected !== undefined} width="380px">
    <DeckList>
      {#if !first}
        <div aria-hidden="true">
          {#each Array(data.pageSize) as _, i (i)}
            <div class="row-skeleton">
              <Skeleton variant="block" width="26px" height="26px" />
              <Skeleton width="18ch" />
            </div>
          {/each}
        </div>
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
      {:else if term && !issue}
        <EmptyState title={t('admin.audit.emptyMatch')} body={t('admin.audit.emptyMatchBody')} />
      {:else if !issue}
        <EmptyState title={t('admin.audit.empty')} />
      {/if}

      <div class="page-controls">
        <div class="page-control" class:visible={hasMore} inert={!hasMore} aria-hidden={!hasMore}>
          <Button
            variant="ghost"
            busy={more?.busy}
            onclick={() => page !== undefined && loadMore(page, loaded)}
          >
            {t('admin.audit.loadMore')}
          </Button>
        </div>
        <div class="page-control" class:visible={atCap} inert={!atCap} aria-hidden={!atCap}>
          <Text size="xs" tone="muted" mono>{t('admin.audit.pageCap', { max: String(data.maxPages) })}</Text>
        </div>
      </div>
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
  </DeckLayout>
</section>

<style>
  .stats {
    inline-size: min(19ch, 50%);
    text-align: end;
  }

  .kind-label {
    flex: 1;
    min-width: 0;
  }

  .kind-count {
    flex: 0 0 4ch;
    text-align: end;
    font-variant-numeric: tabular-nums;
  }

  .row-skeleton {
    display: flex;
    align-items: center;
    gap: var(--bb-space-3);
    padding-inline: 14px;
    min-height: 63px;
    border-bottom: 1px solid var(--bb-border);
  }

  .page-controls {
    display: grid;
    place-items: center;
    padding: var(--bb-space-2);
    text-align: center;
  }

  .page-control {
    grid-area: 1 / 1;
    opacity: 0;
    transition: opacity var(--bb-dur-base);
  }

  .page-control.visible {
    opacity: 1;
  }
</style>
