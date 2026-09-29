<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Select } from '@bagel/kit';
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { isShortcut } from '@bagel/ui/lib/hotkeys';
  import {
    Button,
    SearchInput,
    Field,
    PageHead,
    Scroller,
    ConfirmDialog,
    toast,
    getI18n,
    MasterToggle,
    PageToolbar,
    AlertBanner,
    Card,
    Eyebrow,
    Fact,
    FactList,
    Heading,
    InspectorSurface,
    Text,
    DeckLayout,
    DeckList,
    EmptyState,
    moduleDef,
    actionPayload,
    toastFailure,
    type ActionOk,
  } from '@bagel/kit';
  import type { QuoteView } from '$lib/server/quotes-store';
  import QuoteRow from '$lib/components/quotes/QuoteRow.svelte';
  import QuoteEditor from '$lib/components/quotes/QuoteEditor.svelte';
  import ModuleCommandList from '$lib/components/modules/ModuleCommandList.svelte';

  let { data } = $props();
  const { t } = getI18n();

  // svelte-ignore state_referenced_locally
  let quotes = $state<QuoteView[]>(data.quotes ?? []);
  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let addPerm = $state<string>(data.addPerm ?? 'mod');
  // svelte-ignore state_referenced_locally
  let editPerm = $state<string>(data.editPerm ?? 'mod');
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      quotes = data.quotes ?? [];
      enabled = data.enabled ?? false;
      addPerm = data.addPerm ?? 'mod';
      editPerm = data.editPerm ?? 'mod';
    }
  });

  const quoteCommands = moduleDef('quotes')?.commands ?? [];

  const permOptions = [
    { value: 'mod', label: t('quotes.permMod') },
    { value: 'vip', label: t('quotes.permVip') },
    { value: 'sub', label: t('quotes.permSub') },
    { value: 'everyone', label: t('quotes.permEveryone') }
  ];

  let search = $state('');
  const searching = $derived(search.trim().length > 0);
  const rows = $derived(
    quotes
      .filter((q) => {
        const needle = search.trim().toLowerCase();
        if (!needle) return true;
        return (
          String(q.number).includes(needle) ||
          q.text.toLowerCase().includes(needle) ||
          (q.added_by ?? '').toLowerCase().includes(needle)
        );
      })
      .toSorted((a, b) => b.number - a.number)
  );

  type QuoteActionOk = ActionOk & { quote?: QuoteView; number?: number };
  type QuoteDraft = { text: string; quoteDate: string };
  const failed = toastFailure(toast, t);

  function todayInput(): string {
    const now = new Date();
    const year = now.getFullYear();
    const month = String(now.getMonth() + 1).padStart(2, '0');
    const day = String(now.getDate()).padStart(2, '0');
    return `${year}-${month}-${day}`;
  }

  function formatDate(iso: string): string {
    const parts = iso.slice(0, 10).split('-').map(Number);
    if (parts.length !== 3 || parts.some((part) => !Number.isFinite(part))) return '';
    return new Date(parts[0], parts[1] - 1, parts[2]).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'long',
      day: 'numeric'
    });
  }

  function snippet(text: string): string {
    const clean = text.trim();
    return clean.length > 48 ? `${clean.slice(0, 48).trimEnd()}…` : clean;
  }

  const NEW = '__new__';
  let expanded = $state<string | null>(null);
  let quoteDraft = $state<QuoteDraft | null>(null);
  let editTarget = $state<number | null>(null);
  let adding = $state(false);
  const selectedQuote = $derived(
    expanded && expanded !== NEW ? (quotes.find((quote) => String(quote.number) === expanded) ?? null) : null
  );
  const inspectorOpen = $derived(quoteDraft !== null || selectedQuote !== null);
  const editing = $derived(expanded === NEW || editTarget !== null);
  function inspectorTitleKey(): string {
    if (expanded === NEW) return 'quotes.newQuote';
    if (editTarget !== null) return 'quotes.editQuote';
    return selectedQuote ? 'quotes.quoteDetails' : 'quotes.inspector';
  }
  const inspectorTitle = $derived(t(inspectorTitleKey()));

  function openNew() {
    editTarget = null;
    quoteDraft = { text: '', quoteDate: todayInput() };
    expanded = NEW;
  }

  function openQuote(quote: QuoteView) {
    if (expanded === String(quote.number)) {
      closeInspector();
      return;
    }
    quoteDraft = null;
    editTarget = null;
    expanded = String(quote.number);
  }

  function openEdit(quote: QuoteView) {
    editTarget = quote.number;
    quoteDraft = { text: quote.text, quoteDate: quote.created_at.slice(0, 10) };
  }

  function closeEditor() {
    quoteDraft = null;
    editTarget = null;
  }

  function closeInspector() {
    expanded = null;
    quoteDraft = null;
    editTarget = null;
  }

  const addSubmit: SubmitFunction = () => {
    if (!quoteDraft?.text.trim() || !quoteDraft.quoteDate) return;
    adding = true;
    return async ({ result }) => {
      adding = false;
      const payload = actionPayload<QuoteActionOk>(result);
      if (result.type === 'success' && payload?.ok) {
        closeInspector();
        toast('ok', t('quotes.toastAdded'));
        await invalidateAll();
        return;
      }
      failed(payload, 'quotes.toastAddFailed');
    };
  };

  const editSubmit: SubmitFunction = () => {
    if (!quoteDraft?.text.trim()) return;
    adding = true;
    return async ({ result }) => {
      adding = false;
      const payload = actionPayload<QuoteActionOk>(result);
      if (result.type === 'success' && payload?.ok && payload.quote) {
        const updated = payload.quote;
        quotes = quotes.map((quote) => (quote.number === updated.number ? updated : quote));
        closeEditor();
        toast('ok', t('quotes.toastEdited'));
        await invalidateAll();
        return;
      }
      failed(payload, 'quotes.toastEditFailed');
    };
  };

  function permSubmitFor(get: () => string, set: (value: string) => void): SubmitFunction {
    return () => {
      const was = get();
      return async ({ result }) => {
        const payload = actionPayload<QuoteActionOk>(result);
        if (result.type === 'success' && payload?.ok) return;
        set(was);
        failed(payload, 'quotes.toastPermFailed');
      };
    };
  }
  let addPermForm = $state<HTMLFormElement | null>(null);
  let editPermForm = $state<HTMLFormElement | null>(null);
  const addPermSubmit = permSubmitFor(
    () => addPerm,
    (value) => (addPerm = value)
  );
  const editPermSubmit = permSubmitFor(
    () => editPerm,
    (value) => (editPerm = value)
  );
  function onAddPermChange(e: Event) {
    addPerm = (e.currentTarget as HTMLSelectElement).value;
    addPermForm?.requestSubmit();
  }
  function onEditPermChange(e: Event) {
    editPerm = (e.currentTarget as HTMLSelectElement).value;
    editPermForm?.requestSubmit();
  }

  let deleteTarget = $state<QuoteView | null>(null);
  let deleting = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);
  const deleteSubmit: SubmitFunction = () => {
    deleting = true;
    return async ({ result }) => {
      deleting = false;
      const target = deleteTarget;
      deleteTarget = null;
      const payload = actionPayload<QuoteActionOk>(result);
      if (result.type === 'success' && payload?.ok) {
        if (target) {
          quotes = quotes.filter((quote) => quote.number !== target.number);
          if (expanded === String(target.number)) closeInspector();
          toast('ok', t('quotes.toastDeleted'));
        }
        await invalidateAll();
        return;
      }
      failed(payload, 'quotes.toastDeleteFailed');
    };
  };

  function onKey(e: KeyboardEvent) {
    if (!isShortcut(e, { alt: true })) return;
    if (e.key === '/') {
      e.preventDefault();
      document.getElementById('quotes-search')?.focus();
    } else if (e.key === 'n' || e.key === 'N') {
      e.preventDefault();
      openNew();
    }
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('quotes.eyebrow')} description={t('quotes.description')}>
    {t('quotes.titlePre')}<em>{t('quotes.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('quotes.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      <MasterToggle
        action="?/toggle"
        bind:enabled
        label={t('quotes.botOn')}
        hint={t('quotes.botOnHint')}
        ariaLabel={t('quotes.botOn')}
        failMessage={t('quotes.toastToggleFailed')}
      />
    {/snippet}
    {#snippet trail()}
      <div class="toolbar-actions">
        <div class="toolbar-search">
          <SearchInput id="quotes-search" aria-label={t('quotes.searchLabel')} autocomplete="off"
            placeholder={t('quotes.searchLabel')} clearLabel={t('quotes.searchClear')} bind:value={search} fill />
        </div>

        <Button variant="primary" onclick={openNew} disabled={expanded === NEW}>
          {t('quotes.newQuote')}
        </Button>
      </div>
    {/snippet}
  </PageToolbar>

  <section class="block" aria-labelledby="quotes-perms-h">
    <Heading level={6} as="h2" variant="title" id="quotes-perms-h">{t('quotes.permsTitle')}</Heading>
    <Card>
      <Text size="xs" tone="muted">{t('quotes.permsHint')}</Text>
      <div class="perm-grid">
        <form method="POST" action="?/perm" use:enhance={addPermSubmit} bind:this={addPermForm}>
          <input type="hidden" name="kind" value="add" />
          <Field label={t('quotes.permLabel')}>
            <Select
              fill
              name="perm" value={addPerm} onchange={onAddPermChange}
              options={permOptions}
            />
          </Field>
        </form>

        <form method="POST" action="?/perm" use:enhance={editPermSubmit} bind:this={editPermForm}>
          <input type="hidden" name="kind" value="edit" />
          <Field label={t('quotes.permEditLabel')}>
            <Select
              fill
              name="perm" value={editPerm} onchange={onEditPermChange}
              options={permOptions}
            />
          </Field>
        </form>
      </div>
    </Card>
  </section>

  <p class="bb-sr-only" role="status" aria-live="polite">
    {searching ? t('quotes.resultsCount', { n: rows.length }) : ''}
  </p>

  <DeckLayout inspecting width={editing ? '420px' : '300px'}>
    <DeckList>
      {#if rows.length}
        <ul class="bb-list" aria-label={t('quotes.listLabel')}>
          {#each rows as quote (quote.number)}
            <QuoteRow
              {quote}
              expanded={expanded === String(quote.number)}
              onExpand={() => openQuote(quote)}
              onDelete={() => (deleteTarget = quote)}
            />
          {/each}
        </ul>
      {:else if quotes.length === 0}
        <EmptyState title={t('quotes.emptyTitle')} body={t('quotes.emptySub')}>
          <Button variant="primary" onclick={openNew}>{t('quotes.newQuote')}</Button>
        </EmptyState>
      {:else}
        <EmptyState title={t('quotes.noneMatch')} />
      {/if}
    </DeckList>

    <InspectorSurface
      open={inspectorOpen}
      title={inspectorTitle}
      controls="quote-inspector"
      closeLabel={t('common.cancel')}
      onClose={closeInspector}
    >
      {#if quoteDraft}
        <Scroller fill padding="16px" smooth>
          <QuoteEditor
            bind:draft={quoteDraft}
            number={editTarget}
            busy={adding}
            onCancel={editTarget !== null ? closeEditor : closeInspector}
            onSubmit={editTarget !== null ? editSubmit : addSubmit}
          />
        </Scroller>
      {:else if selectedQuote}
        <Scroller fill padding="18px" smooth>
          <div class="quote-detail">
            <Eyebrow as="div">#{selectedQuote.number}</Eyebrow>
            <blockquote class="quote-body"><Text>{selectedQuote.text}</Text></blockquote>
            <FactList>
              <Fact term={t('quotes.fieldDay')}>{formatDate(selectedQuote.created_at)}</Fact>
              {#if selectedQuote.added_by}
                <Fact term={t('quotes.addedBy')}>@{selectedQuote.added_by}</Fact>
              {/if}
            </FactList>
            <div class="detail-actions">
              <Button variant="primary" onclick={() => selectedQuote && openEdit(selectedQuote)}>
                {t('quotes.editBtnShort')}
              </Button>
              <Button variant="destructive" onclick={() => (deleteTarget = selectedQuote)}>
                {t('quotes.del')}
              </Button>
            </div>
          </div>
        </Scroller>
      {/if}
      {#snippet idle()}
        <Text size="sm" tone="muted">{t('quotes.inspectorIdle')}</Text>
        <Button variant="ghost" onclick={openNew}>{t('quotes.newQuote')}</Button>
      {/snippet}
    </InspectorSurface>
  </DeckLayout>

  {#if quoteCommands.length}
    <div class="cmd-block">
      <DeckList>
        <ModuleCommandList moduleId="quotes" commands={quoteCommands} headingId="quotes-cmds-h" />
      </DeckList>
    </div>
  {/if}
</section>

<svelte:window onkeydown={onKey} />

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('quotes.deleteTitle')}
  body={deleteTarget ? t('quotes.deleteBodyNamed', { snippet: snippet(deleteTarget.text) }) : undefined}
  confirmLabel={t('quotes.del')}
  cancelLabel={t('common.cancel')}
  danger
  busy={deleting}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/delete" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="number" value={deleteTarget?.number ?? ''} />
</form>

<style>
  .toolbar-actions { display: flex; align-items: center; gap: 12px; }

  .block { display: grid; gap: 12px; margin-bottom: 26px; }
  .perm-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 280px));
    gap: 16px;
    margin-top: 14px;
  }

  .cmd-block { margin-top: 26px; }

  .toolbar-search { width: 220px; --input-w: 100%; }

  .quote-detail { display: flex; flex-direction: column; gap: 18px; }
  .quote-body {
    margin: 0;
    padding: 0 0 0 14px;
    border-left: 2px solid var(--bb-tan);
    overflow-wrap: anywhere;
  }
  .detail-actions { display: flex; gap: 10px; align-self: flex-start; margin-top: 4px; }

  @media (max-width: 680px) {
    .toolbar-actions { width: 100%; flex-wrap: wrap; }
    .toolbar-search { width: 100%; order: 3; }
    .perm-grid { grid-template-columns: 1fr; }
  }
</style>
