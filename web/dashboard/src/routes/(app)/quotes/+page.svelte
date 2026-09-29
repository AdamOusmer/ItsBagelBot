<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Select } from '@bagel/kit';
  import { untrack } from 'svelte';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { isShortcut } from '@bagel/ui/lib/hotkeys';
  import {
    Button,
    createDiscardGuard,
    EditorFooter,
    InspectorSurface,
    focusFirstInvalid,
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
    Heading,
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
      savedPerm = { add: addPerm, edit: editPerm };
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

  function snippet(text: string): string {
    const clean = text.trim();
    return clean.length > 48 ? `${clean.slice(0, 48).trimEnd()}…` : clean;
  }

  const NEW = '__new__';
  const inspector = createInspector<QuoteDraft>();
  let draft = $state<QuoteDraft | null>(null);
  let busy = $state(false);
  let validationAttempted = $state(false);
  let formEl = $state<HTMLFormElement | null>(null);

  $effect(() => {
    const snap = draft ? { ...draft } : null;
    if (snap) untrack(() => inspector.edit(snap));
  });

  const creating = $derived(inspector.selectedId === NEW);
  const canSave = $derived(creating || inspector.dirty);
  const selectedQuote = $derived(
    inspector.selectedId && !creating
      ? (quotes.find((quote) => String(quote.number) === inspector.selectedId) ?? null)
      : null
  );

  const discard = createDiscardGuard(() => inspector.dirty, () => {
    inspector.reset();
    draft = null;
  });
  const guarded = discard.guard;

  function present(id: string, next: QuoteDraft) {
    validationAttempted = false;
    inspector.open(id, { ...next });
    draft = { ...next };
  }
  function openNew() {
    guarded(() => present(NEW, { text: '', quoteDate: todayInput() }));
  }
  function openQuote(quote: QuoteView) {
    if (inspector.selectedId === String(quote.number)) {
      closeInspector();
      return;
    }
    guarded(() => present(String(quote.number), { text: quote.text, quoteDate: quote.created_at.slice(0, 10) }));
  }
  function closeInspector() {
    guarded(() => {
      validationAttempted = false;
      inspector.reset();
      draft = null;
    });
  }
  function clearSearch() {
    search = '';
  }

  function invalidDraft(d: QuoteDraft | null): boolean {
    return !d || !d.text.trim() || !/^\d{4}-\d{2}-\d{2}$/.test(d.quoteDate);
  }

  const saveSubmit: SubmitFunction = (input) => {
    if (invalidDraft(draft)) {
      validationAttempted = true;
      input.cancel();
      void focusFirstInvalid(formEl);
      return;
    }
    const requestId = inspector.beginSave()?.requestId;
    const wasCreating = creating;
    busy = true;
    return async ({ result }) => {
      busy = false;
      const payload = actionPayload<QuoteActionOk>(result);
      const ok = result.type === 'success' && payload?.ok === true;
      const applied = requestId ? inspector.resolved(requestId, { type: ok ? 'success' : 'error' }) : false;
      if (!ok) {
        failed(payload, wasCreating ? 'quotes.toastAddFailed' : 'quotes.toastEditFailed');
        return;
      }
      toast('ok', t(wasCreating ? 'quotes.toastAdded' : 'quotes.toastEdited'));
      if (wasCreating && applied) {
        inspector.reset();
        draft = null;
      } else if (payload?.quote) {
        const updated = payload.quote;
        quotes = quotes.map((quote) => (quote.number === updated.number ? updated : quote));
      }
      await invalidateAll();
    };
  };

  let permPending = $state<'add' | 'edit' | null>(null);
  // svelte-ignore state_referenced_locally
  let savedPerm = { add: data.addPerm ?? 'mod', edit: data.editPerm ?? 'mod' };
  function permSubmitFor(kind: 'add' | 'edit', set: (value: string) => void): SubmitFunction {
    return () => {
      permPending = kind;
      return async ({ result }) => {
        permPending = null;
        const payload = actionPayload<QuoteActionOk>(result);
        if (result.type === 'success' && payload?.ok) {
          savedPerm[kind] = kind === 'add' ? addPerm : editPerm;
          return;
        }
        set(savedPerm[kind]);
        failed(payload, 'quotes.toastPermFailed');
      };
    };
  }
  let addPermForm = $state<HTMLFormElement | null>(null);
  let editPermForm = $state<HTMLFormElement | null>(null);
  const addPermSubmit = permSubmitFor('add', (value) => (addPerm = value));
  const editPermSubmit = permSubmitFor('edit', (value) => (editPerm = value));
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
          if (inspector.selectedId === String(target.number)) {
            inspector.reset();
            draft = null;
          }
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

        <Button variant="primary" onclick={openNew} disabled={creating}>
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
              name="perm" value={addPerm} onchange={onAddPermChange} disabled={permPending !== null}
              aria-busy={permPending === 'add'}
              options={permOptions}
            />
          </Field>
        </form>

        <form method="POST" action="?/perm" use:enhance={editPermSubmit} bind:this={editPermForm}>
          <input type="hidden" name="kind" value="edit" />
          <Field label={t('quotes.permEditLabel')}>
            <Select
              fill
              name="perm" value={editPerm} onchange={onEditPermChange} disabled={permPending !== null}
              aria-busy={permPending === 'edit'}
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

  <DeckLayout inspecting={inspector.isOpen}>
    <DeckList>
      {#if rows.length}
        <ul class="bb-list" aria-label={t('quotes.listLabel')}>
          {#each rows as quote (quote.number)}
            <QuoteRow
              {quote}
              expanded={inspector.selectedId === String(quote.number)}
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
        <EmptyState title={t('quotes.noneMatch')}>
          <Button variant="secondary" onclick={clearSearch}>{t('quotes.searchClear')}</Button>
        </EmptyState>
      {/if}
    </DeckList>

    {#if inspector.isOpen && draft}
      <InspectorSurface
        open
        title={creating ? t('quotes.newQuote') : t('quotes.editQuote')}
        controls="quote-inspector"
        closeLabel={t('common.cancel')}
        onClose={closeInspector}
      >
        <form
          method="POST"
          action={creating ? '?/add' : '?/edit'}
          novalidate
          use:enhance={saveSubmit}
          class="inspector-form"
          bind:this={formEl}
        >
          {#if selectedQuote}
            <input type="hidden" name="number" value={selectedQuote.number} />
          {/if}
          <Scroller fill padding="16px" smooth>
            {#key inspector.selectedId}
              <QuoteEditor bind:draft attempted={validationAttempted} addedBy={selectedQuote?.added_by ?? ''} />
            {/key}
          </Scroller>
          <EditorFooter
            status={inspector.status}
            dirty={inspector.dirty}
            {canSave}
            saveLabel={creating ? t('quotes.addBtn') : t('quotes.editBtn')}
            cancelLabel={t('common.cancel')}
            savingLabel={t('quotes.saving')}
            savedLabel={t('quotes.saved')}
            errorLabel={t(creating ? 'quotes.toastAddFailed' : 'quotes.toastEditFailed')}
            dirtyLabel={t('quotes.unsavedChanges')}
            onCancel={closeInspector}
          />
        </form>
      </InspectorSurface>
    {/if}
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
  open={discard.open}
  title={t('quotes.discardTitle')}
  body={t('quotes.discardBody')}
  confirmLabel={t('quotes.discard')}
  cancelLabel={t('quotes.keepEditing')}
  danger
  onCancel={discard.cancel}
  onConfirm={discard.confirm}
/>
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

  .inspector-form { display: flex; flex-direction: column; min-height: 0; flex: 1; }

  @media (max-width: 680px) {
    .toolbar-actions { width: 100%; flex-wrap: wrap; }
    .toolbar-search { width: 100%; order: 3; }
    .perm-grid { grid-template-columns: 1fr; }
  }
</style>
