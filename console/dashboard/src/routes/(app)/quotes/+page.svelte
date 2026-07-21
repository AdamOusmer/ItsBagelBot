<script lang="ts">
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    Button,
    Field,
    PageHead,
    Scroller,
    ConfirmDialog,
    toast,
    getI18n,
    MasterToggle,
    PageToolbar,
    AlertBanner,
    DeckList,
    EmptyState,
    moduleDef
  } from '@bagel/shared';
  import type { QuoteView } from '$lib/server/quotes-store';
  import QuoteRow from '$lib/components/quotes/QuoteRow.svelte';
  import QuoteEditor from '$lib/components/quotes/QuoteEditor.svelte';
  import ModuleCommandRow from '$lib/components/modules/ModuleCommandRow.svelte';

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

  // The chat-commands reference rendered below the book, straight from the
  // module catalog so it never drifts from the generic /modules/[id] page.
  const quoteCommands = moduleDef('quotes')?.commands ?? [];

  const permOptions = [
    { value: 'mod', label: t('quotes.permMod') },
    { value: 'vip', label: t('quotes.permVip') },
    { value: 'sub', label: t('quotes.permSub') },
    { value: 'everyone', label: t('quotes.permEveryone') }
  ];

  // --- Search over number / text / author, newest number first --------------
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

  type ActionResult = { ok?: boolean; error?: string; quote?: QuoteView; number?: number };
  type QuoteDraft = { text: string; quoteDate: string };

  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  function failed(payload: ActionResult | undefined, fallbackKey: string) {
    toast('err', payload?.error ?? t(fallbackKey));
  }

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

  // Short snippet of a quote for the delete confirmation, so it names what it
  // removes rather than an anonymous "this quote".
  function snippet(text: string): string {
    const clean = text.trim();
    return clean.length > 48 ? `${clean.slice(0, 48).trimEnd()}…` : clean;
  }

  const NEW = '__new__';
  let expanded = $state<string | null>(null);
  let quoteDraft = $state<QuoteDraft | null>(null);
  // The quote number being rewritten; null while the editor adds a new one.
  let editTarget = $state<number | null>(null);
  let adding = $state(false);
  const selectedQuote = $derived(
    expanded && expanded !== NEW ? (quotes.find((quote) => String(quote.number) === expanded) ?? null) : null
  );

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

  // openEdit swaps the inspector's detail pane for the editor, prefilled with
  // the quote's current text and day.
  function openEdit(quote: QuoteView) {
    editTarget = quote.number;
    quoteDraft = { text: quote.text, quoteDate: quote.created_at.slice(0, 10) };
  }

  // closeEditor returns from the edit form to the detail pane (the inspector
  // stays open on the same quote).
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
      const payload = payloadOf(result);
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
      const payload = payloadOf(result);
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

  // One revert-on-failure submit per perm select; both post ?/perm with a
  // hidden kind field naming the gate they write.
  function permSubmitFor(get: () => string, set: (value: string) => void): SubmitFunction {
    return () => {
      const was = get();
      return async ({ result }) => {
        const payload = payloadOf(result);
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
      const payload = payloadOf(result);
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

  // Keyboard: Escape closes the inspector (owned here). Alt+/ focuses search and
  // Alt+N adds a quote; neither fires while typing, and Alt keeps clear of the
  // browser's single-key shortcuts.
  function isTyping(e: KeyboardEvent): boolean {
    const el = e.target as HTMLElement | null;
    return (
      !!el &&
      (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' || el.isContentEditable)
    );
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && expanded) {
      // Editing steps back to the quote's detail pane; otherwise close.
      if (editTarget !== null) closeEditor();
      else closeInspector();
      return;
    }
    if (isTyping(e) || e.ctrlKey || e.metaKey || !e.altKey) return;
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
          <label for="quotes-search" class="sr-only">{t('quotes.searchLabel')}</label>
          <div class="search">
            <Icon name="search" size={15} />
            <input
              id="quotes-search"
              type="text"
              autocomplete="off"
              placeholder={t('quotes.searchLabel')}
              bind:value={search}
            />
            {#if search}
              <button type="button" class="search-clear" aria-label={t('quotes.searchClear')} onclick={() => (search = '')}>
                <Icon name="x" size={12} />
              </button>
            {/if}
          </div>
        </div>

        <Button variant="primary" icon="plus" onclick={openNew} disabled={expanded === NEW}>
          {t('quotes.newQuote')}
        </Button>
      </div>
    {/snippet}
  </PageToolbar>

  <!-- Chat permissions: who may save or rewrite from chat. Selects save on
       change; each form names the gate it writes via its hidden kind field. -->
  <section class="block" aria-labelledby="quotes-perms-h">
    <h2 id="quotes-perms-h" class="block-title">{t('quotes.permsTitle')}</h2>
    <div class="card">
      <p class="hint">{t('quotes.permsHint')}</p>
      <div class="perm-grid">
        <form method="POST" action="?/perm" use:enhance={addPermSubmit} bind:this={addPermForm}>
          <input type="hidden" name="kind" value="add" />
          <Field label={t('quotes.permLabel')}>
            <select class="search" name="perm" value={addPerm} onchange={onAddPermChange}>
              {#each permOptions as option (option.value)}
                <option value={option.value}>{option.label}</option>
              {/each}
            </select>
          </Field>
        </form>

        <form method="POST" action="?/perm" use:enhance={editPermSubmit} bind:this={editPermForm}>
          <input type="hidden" name="kind" value="edit" />
          <Field label={t('quotes.permEditLabel')}>
            <select class="search" name="perm" value={editPerm} onchange={onEditPermChange}>
              {#each permOptions as option (option.value)}
                <option value={option.value}>{option.label}</option>
              {/each}
            </select>
          </Field>
        </form>
      </div>
    </div>
  </section>

  <!-- Polite live region: announces the match count as the search narrows. -->
  <p class="sr-only" role="status" aria-live="polite">
    {searching ? t('quotes.resultsCount', { n: rows.length }) : ''}
  </p>

  <div class="deck" class:inspecting={expanded === NEW || editTarget !== null}>
    <DeckList>
      {#if rows.length}
        <ul class="list" aria-label={t('quotes.listLabel')}>
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
        <EmptyState icon="quote" title={t('quotes.emptyTitle')} body={t('quotes.emptySub')}>
          <Button variant="primary" icon="plus" onclick={openNew}>{t('quotes.newQuote')}</Button>
        </EmptyState>
      {:else}
        <EmptyState icon="search" title={t('quotes.noneMatch')} />
      {/if}
    </DeckList>

    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="inspector-backdrop"
      class:open={expanded !== null}
      role="presentation"
      onclick={closeInspector}
      onkeydown={(e) => {
        if (e.key === 'Enter') closeInspector();
      }}
    ></div>
    <aside id="quote-inspector" class="inspector" class:open={expanded !== null} aria-label={t('quotes.inspector')}>
      <div class="inspector-head">
        <span class="inspector-tag">
          {expanded === NEW
            ? t('quotes.newQuote')
            : editTarget !== null
              ? t('quotes.editQuote')
              : selectedQuote
                ? t('quotes.quoteDetails')
                : t('quotes.inspector')}
        </span>
        {#if expanded}
          <button class="mini" type="button" aria-label={t('common.cancel')} onclick={closeInspector}>
            <Icon name="x" size={14} />
          </button>
        {/if}
      </div>

      {#if quoteDraft}
        <Scroller fill padding="16px" data-lenis-prevent>
          <QuoteEditor
            bind:draft={quoteDraft}
            number={editTarget}
            busy={adding}
            onCancel={editTarget !== null ? closeEditor : closeInspector}
            onSubmit={editTarget !== null ? editSubmit : addSubmit}
          />
        </Scroller>
      {:else if selectedQuote}
        <Scroller fill padding="18px" data-lenis-prevent>
          <div class="quote-detail">
            <div class="quote-number">#{selectedQuote.number}</div>
            <blockquote>{selectedQuote.text}</blockquote>
            <dl>
              <div>
                <dt>{t('quotes.fieldDay')}</dt>
                <dd>{formatDate(selectedQuote.created_at)}</dd>
              </div>
              {#if selectedQuote.added_by}
                <div>
                  <dt>{t('quotes.addedBy')}</dt>
                  <dd>@{selectedQuote.added_by}</dd>
                </div>
              {/if}
            </dl>
            <div class="detail-actions">
              <Button variant="primary" icon="edit" onclick={() => selectedQuote && openEdit(selectedQuote)}>
                {t('quotes.editBtnShort')}
              </Button>
              <Button variant="destructive" icon="trash" onclick={() => (deleteTarget = selectedQuote)}>
                {t('quotes.del')}
              </Button>
            </div>
          </div>
        </Scroller>
      {:else}
        <div class="inspector-idle">
          <span class="idle-glyph"><Icon name="quote" size={18} /></span>
          <p>{t('quotes.inspectorIdle')}</p>
          <Button variant="ghost" icon="plus" onclick={openNew}>{t('quotes.newQuote')}</Button>
        </div>
      {/if}
    </aside>
  </div>

  <!-- Chat-commands reference: the same read-only rows the generic module
       pages list, fed from the shared catalog def. -->
  {#if quoteCommands.length}
    <section class="block cmd-block" aria-labelledby="quotes-cmds-h">
      <div class="cmd-head">
        <h2 id="quotes-cmds-h" class="block-title">{t('modules.commandsTitle')}</h2>
        <span class="cmd-hint">{t('modules.commandsHint')}</span>
      </div>
      <DeckList>
        <ul class="list" aria-label={t('modules.commandsTitle')}>
          {#each quoteCommands as command, i (command.trigger)}
            <li><ModuleCommandRow {command} index={i + 1} /></li>
          {/each}
        </ul>
      </DeckList>
    </section>
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

  /* Chat-permissions block, mirroring the loyalty page's section shells so
     settings read as their own airy card instead of crowding the toolbar. */
  .block { margin-bottom: 26px; }
  .block-title {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0 0 12px;
  }
  .card {
    padding: 18px;
    border: 1px solid var(--bb-border);
    border-radius: 10px;
    background: rgba(240, 236, 228, 0.02);
  }
  .hint { margin: 0 0 14px; font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }
  .perm-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 280px));
    gap: 16px;
  }
  .perm-grid select {
    width: 100%;
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-white);
    background: var(--bb-bg-1, #16130f);
    border: 1px solid var(--rule);
    border-radius: 7px;
    padding: 8px 10px;
  }

  /* Chat-commands reference below the book. */
  .cmd-block { margin-top: 26px; }
  .cmd-head { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
  .cmd-hint { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  .toolbar-search { width: 220px; }
  .toolbar-search .search { width: 100%; }
  .search-clear {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    flex: none;
    border: none;
    background: transparent;
    color: var(--bb-muted);
    cursor: pointer;
    border-radius: 8px;
  }
  .search-clear:hover { color: var(--bb-white); }

  .deck {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  @media (min-width: 1080px) {
    .deck { grid-template-columns: minmax(0, 1fr) 300px; }
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 420px; }
  }
  .list { list-style: none; margin: 0; padding: 0; }
  .list :global(.row-shell:last-child) { border-bottom: none; }

  .inspector {
    position: sticky;
    top: 62px;
    border: 1px solid var(--rule);
    border-top-color: var(--rule-strong);
    border-radius: 8px;
    background: linear-gradient(180deg, rgba(240, 236, 228, 0.03), rgba(240, 236, 228, 0.012));
    display: flex;
    flex-direction: column;
    max-height: calc(100vh - 62px - 108px);
  }
  .inspector-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--rule);
  }
  .inspector-tag {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 12px;
    letter-spacing: 0.02em;
    color: var(--bb-tan);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .inspector-idle {
    padding: 34px 20px;
    text-align: center;
    color: var(--bb-muted);
    font-family: var(--bb-font-body);
    font-size: 13px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
  }
  .idle-glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 40px;
    height: 40px;
    border: 1px solid var(--rule-tan);
    border-radius: 8px;
    color: var(--bb-tan-light);
  }
  .inspector-idle p { margin: 0; max-width: 26ch; line-height: 1.5; }

  .quote-detail { display: flex; flex-direction: column; gap: 18px; }
  .quote-number {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-tan);
  }
  blockquote {
    margin: 0;
    padding: 0 0 0 14px;
    border-left: 2px solid var(--bb-tan);
    font-family: var(--bb-font-body);
    font-size: 15px;
    line-height: 1.6;
    color: var(--bb-white);
    overflow-wrap: anywhere;
  }
  dl { display: flex; flex-direction: column; gap: 12px; margin: 0; }
  dl div { display: flex; align-items: baseline; justify-content: space-between; gap: 14px; }
  dt { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }
  dd { margin: 0; font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-tan-light); text-align: right; }
  .detail-actions { display: flex; gap: 10px; align-self: flex-start; margin-top: 4px; }
  .inspector-backdrop { display: none; }

  @media (max-width: 1079px) {
    .inspector { display: none; }
    .inspector.open {
      display: flex;
      position: fixed;
      left: 0;
      right: 0;
      bottom: 0;
      top: auto;
      z-index: 220;
      max-height: 88vh;
      border-radius: 8px 8px 0 0;
      background: var(--bb-bg-1, #111);
      animation: sheet-in var(--bb-dur-base, 320ms) var(--bb-ease-out-expo, cubic-bezier(.16,1,.3,1)) both;
    }
    .inspector-backdrop.open {
      display: block;
      position: fixed;
      inset: 0;
      z-index: 219;
      background: rgba(0, 0, 0, 0.55);
    }
    @keyframes sheet-in { from { transform: translateY(100%); } to { transform: translateY(0); } }
  }

  @media (max-width: 680px) {
    .toolbar-actions { width: 100%; flex-wrap: wrap; }
    .toolbar-search { width: 100%; order: 3; }
    .perm-grid { grid-template-columns: 1fr; }
  }
</style>
