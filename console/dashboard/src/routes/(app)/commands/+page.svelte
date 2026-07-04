<script lang="ts">
  import { deserialize } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    Card,
    PageHead,
    Scroller,
    toast,
    normName,
    getI18n,
    type CommandView,
    type CommandErrors,
    type Perm
  } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';
  import CommandRow from '$lib/components/commands/CommandRow.svelte';
  import CommandEditor from '$lib/components/commands/CommandEditor.svelte';
  import { loadDraft, clearDraft, hasDraft, type CommandDraft } from '$lib/components/commands/drafts';

  let { data } = $props();

  const { t } = getI18n();

  // Local source of truth, seeded from the SSR load. Each action result is
  // reconciled row-by-row into this list (see applyResult) rather than wholesale
  // replacing it with the server's snapshot: concurrent submits then only touch
  // their own row, so a slower reply built from a pre-flush DB snapshot cannot
  // clobber another in-flight change.
  // svelte-ignore state_referenced_locally
  let items = $state<CommandView[]>(data.commands ?? []);

  // Resync when a fresh SSR load delivers a different list (full reload), but
  // not on optimistic edits — those mutate items without touching data.commands.
  // svelte-ignore state_referenced_locally
  let seed = data.commands;
  $effect(() => {
    if (data.commands !== seed) {
      seed = data.commands;
      items = data.commands ?? [];
    }
  });

  type ActionResult = {
    ok: boolean;
    action?: 'created' | 'updated' | 'deleted';
    name?: string;
    original?: string;
    silent?: boolean;
    error?: string;
    errors?: CommandErrors;
    commands?: CommandView[];
  };

  // Reconcile one action result into the local list. Uses only the affected
  // command (looked up by name) plus the original name on rename, never the
  // whole returned list, so it is order-independent across concurrent results.
  function applyResult(d: ActionResult) {
    if (!d.ok) {
      if (d.error) toast('err', d.error);
      return;
    }
    if (d.action === 'deleted') {
      items = items.filter((c) => c.name !== d.name);
    } else {
      const next = d.commands?.find((c) => c.name === d.name);
      if (next) {
        const without = items.filter((c) => c.name !== d.name && c.name !== d.original);
        items = [...without, next];
      }
    }
    if (!d.silent) {
      const key =
        d.action === 'deleted'
          ? 'commands.toastDeleted'
          : d.action === 'created'
            ? 'commands.toastCreated'
            : 'commands.toastUpdated';
      toast('ok', t(key, { name: d.name ?? '' }));
    }
  }

  // --- Per-row save-state machine ------------------------------------------
  // saving -> saved (server ack) -> live (~2.5s later: the write-behind flush +
  // projector hop have realistically landed) -> idle. Error short-circuits.
  let rowStatus = $state<Record<string, SaveState>>({});
  const statusTimers = new Map<string, ReturnType<typeof setTimeout>[]>();

  function clearTimers(name: string) {
    for (const t of statusTimers.get(name) ?? []) clearTimeout(t);
    statusTimers.delete(name);
  }

  function setStatus(name: string, s: SaveState) {
    clearTimers(name);
    rowStatus = { ...rowStatus, [name]: s };
  }

  function ackSaved(name: string) {
    setStatus(name, 'saved');
    statusTimers.set(name, [
      setTimeout(() => (rowStatus = { ...rowStatus, [name]: 'live' }), 2500),
      setTimeout(() => (rowStatus = { ...rowStatus, [name]: 'idle' }), 7000)
    ]);
  }

  function flagError(name: string) {
    setStatus(name, 'error');
    statusTimers.set(name, [setTimeout(() => (rowStatus = { ...rowStatus, [name]: 'idle' }), 4000)]);
  }

  // --- Filters ---------------------------------------------------------------
  const filters = ['All', 'Active', 'Disabled'] as const;
  // Internal keys drive the filter logic; only the display label is translated.
  const filterLabel = (f: (typeof filters)[number]) =>
    f === 'Active' ? t('commands.filterActive') : f === 'Disabled' ? t('commands.filterDisabled') : t('commands.filterAll');
  let active = $state<(typeof filters)[number]>('All');
  let search = $state('');

  const rows = $derived(
    items
      .filter((c) => (active === 'Disabled' ? !c.is_active : active === 'Active' ? c.is_active : true))
      .filter((c) => {
        const q = search.toLowerCase();
        return (
          c.name.toLowerCase().includes(q) ||
          (c.aliases ?? []).some((a) => a.toLowerCase().includes(q)) ||
          c.response.toLowerCase().includes(q)
        );
      })
      .toSorted((a, b) => a.name.localeCompare(b.name))
  );

  // --- Inline editor ----------------------------------------------------------
  const NEW = '__new__';
  let expanded = $state<string | null>(null);
  let editorDraft = $state<CommandDraft | null>(null);
  let serverErrors = $state<CommandErrors | null>(null);
  let busy = $state(false);
  // Bumped whenever sessionStorage drafts may have changed, so unsaved chips re-derive.
  let draftVersion = $state(0);

  function blankDraft(): CommandDraft {
    return {
      edit: false,
      name: '',
      originalName: '',
      aliases: [],
      response: '',
      perm: 'everyone',
      cooldown: 0,
      allowed_user_id: '',
      stream_online_only: false,
      is_active: true
    };
  }

  function fromView(c: CommandView): CommandDraft {
    return {
      edit: true,
      name: c.name,
      originalName: c.name,
      aliases: [...(c.aliases ?? [])],
      response: c.response,
      perm: (c.perm ?? 'everyone') as Perm,
      cooldown: c.cooldown ?? 0,
      allowed_user_id: c.allowed_user_id ?? '',
      stream_online_only: c.stream_online_only === true,
      is_active: c.is_active
    };
  }

  function openNew() {
    serverErrors = null;
    const restored = loadDraft('', false);
    editorDraft = restored ?? blankDraft();
    expanded = NEW;
    if (restored) toast('info', t('commands.toastRestoreDraft'));
  }

  function openEdit(c: CommandView) {
    if (expanded === c.name) {
      closeEditor();
      return;
    }
    serverErrors = null;
    const restored = loadDraft(c.name, true);
    editorDraft = restored ?? fromView(c);
    expanded = c.name;
    if (restored) toast('info', t('commands.toastRestoreEdits'));
  }

  function closeEditor() {
    expanded = null;
    editorDraft = null;
    serverErrors = null;
    draftVersion++;
  }

  const rowHasDraft = (name: string) => {
    void draftVersion;
    return hasDraft(name);
  };

  // --- Save (optimistic with row-level rollback) -----------------------------
  const saveSubmit: SubmitFunction = () => {
    const d = editorDraft;
    if (!d) return;
    const key = normName(d.name);
    const orig = d.edit ? normName(d.originalName) : undefined;

    // Row-level snapshot: rollback restores only the affected row(s), so a
    // concurrent toggle on another row can't be clobbered.
    const prevRows = items.filter((c) => c.name === key || c.name === orig);
    const optimistic: CommandView = {
      name: key,
      aliases: d.aliases.map(normName).filter(Boolean),
      response: d.response,
      is_active: d.is_active,
      stream_online_only: d.stream_online_only,
      perm: d.perm,
      cooldown: Math.floor(Number(d.cooldown) || 0),
      allowed_user_id: d.allowed_user_id.replace(/\D/g, ''),
      uses: items.find((c) => c.name === (orig ?? key))?.uses
    };
    items = [...items.filter((c) => c.name !== key && c.name !== orig), optimistic];
    busy = true;
    setStatus(key, 'saving');

    return async ({ result }) => {
      busy = false;
      const payload =
        result.type === 'success' || result.type === 'failure'
          ? (result.data as ActionResult | undefined)
          : undefined;

      if (result.type === 'success' && payload?.ok) {
        applyResult({ ...payload, silent: true });
        clearDraft(d.edit ? d.originalName : '', d.edit);
        ackSaved(key);
        closeEditor();
        return;
      }

      // Rollback the affected rows; keep the editor open with the draft intact.
      items = [...items.filter((c) => c.name !== key && c.name !== orig), ...prevRows];
      flagError(orig ?? key);
      serverErrors = payload?.errors ?? null;
      if (payload?.error && !payload.errors) toast('err', payload.error);
      else if (!payload) toast('err', t('commands.toastSaveFailed'));
    };
  };

  // --- Toggle (optimistic flip) ----------------------------------------------
  const toggleSubmit =
    (c: CommandView): SubmitFunction =>
    () => {
      const before = { ...c };
      items = items.map((x) => (x.name === c.name ? { ...x, is_active: !x.is_active } : x));
      setStatus(c.name, 'saving');
      return async ({ result }) => {
        const payload =
          result.type === 'success' || result.type === 'failure'
            ? (result.data as ActionResult | undefined)
            : undefined;
        if (result.type === 'success' && payload?.ok) {
          applyResult(payload); // silent from the server
          ackSaved(c.name);
        } else {
          items = items.map((x) => (x.name === c.name ? before : x));
          flagError(c.name);
          toast('err', payload?.error ?? t('commands.toastToggleFailed'));
        }
      };
    };

  // --- Delete (optimistic removal + undo toast) --------------------------------
  function postAction(action: string, body: FormData): Promise<ActionResult | null> {
    return fetch(`?/${action}`, { method: 'POST', body })
      .then(async (res) => {
        const result = deserialize(await res.text());
        return result.type === 'success' || result.type === 'failure'
          ? ((result.data as ActionResult | undefined) ?? null)
          : null;
      })
      .catch(() => null);
  }

  function formDataFor(c: CommandView): FormData {
    const body = new FormData();
    body.set('name', c.name);
    for (const a of c.aliases ?? []) body.append('aliases', a);
    body.set('response', c.response);
    body.set('perm', c.perm ?? 'everyone');
    body.set('cooldown', String(c.cooldown ?? 0));
    body.set('allowed_user_id', c.allowed_user_id ?? '');
    body.set('stream_online_only', c.stream_online_only ? 'on' : '');
    body.set('is_active', c.is_active ? 'on' : '');
    return body;
  }

  async function restore(snapshot: CommandView) {
    items = [...items.filter((x) => x.name !== snapshot.name), snapshot];
    setStatus(snapshot.name, 'saving');
    const payload = await postAction('save', formDataFor(snapshot));
    if (payload?.ok) {
      applyResult({ ...payload, silent: true });
      ackSaved(snapshot.name);
      toast('ok', t('commands.toastRestored', { name: snapshot.name }));
    } else {
      flagError(snapshot.name);
      toast('err', t('commands.toastCouldNotRestore', { name: snapshot.name }));
    }
  }

  async function requestDelete(c: CommandView) {
    const snapshot = { ...c, aliases: [...(c.aliases ?? [])] };
    items = items.filter((x) => x.name !== c.name);
    if (expanded === c.name) closeEditor();
    clearDraft(c.name, true);
    draftVersion++;

    // The delete RPC is immediate server-side, so undo is a re-create from the
    // snapshot (the save's write-behind flush lands after the delete: safe order).
    let undone = false;
    toast('ok', t('commands.toastDeletedShort', { name: c.name }), {
      undoLabel: t('commands.undo'),
      onUndo: () => {
        undone = true;
        void restore(snapshot);
      }
    });

    const body = new FormData();
    body.set('name', c.name);
    const payload = await postAction('delete', body);
    if (!payload?.ok && !undone) {
      items = [...items.filter((x) => x.name !== snapshot.name), snapshot];
      toast('err', payload?.error ?? t('commands.toastDeleteFailed', { name: c.name }));
    }
  }

  const activeCount = $derived(items.filter((c) => c.is_active).length);

  // --- Keyboard control: "/" jumps to search, "n" starts a new command,
  // Escape closes the inspector. Ignored while typing in any field. ---
  let searchInput = $state<HTMLInputElement | null>(null);

  function isTyping(e: KeyboardEvent): boolean {
    const t = e.target as HTMLElement | null;
    return !!t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable);
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      if (editorDraft) closeEditor();
      return;
    }
    if (isTyping(e) || e.metaKey || e.ctrlKey || e.altKey) return;
    if (e.key === '/') {
      e.preventDefault();
      searchInput?.focus();
    } else if (e.key === 'n' || e.key === 'N') {
      e.preventDefault();
      openNew();
    }
  }
</script>

<section class="screen active">
  <PageHead
    eyebrow={t('commands.eyebrow')}
    description={t('commands.description', { active: activeCount, disabled: items.length - activeCount })}
  >
    {t('commands.titlePre')}<em>{t('commands.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <div class="degraded" role="alert">
      <Icon name="ban" size={13} />
      {t('commands.degraded')}
    </div>
  {/if}

  <div class="toolbar">
    <div class="chip-row">
      {#each filters as f}
        <button class="chip {active === f ? 'on' : ''}" onclick={() => (active = f)}>{filterLabel(f)}</button>
      {/each}
    </div>
    <div class="grow"></div>
    <span class="keys" aria-hidden="true"><kbd class="hint">/</kbd> {t('commands.keysSearch')} <kbd class="hint">N</kbd> {t('commands.keysNew')}</span>
    <label class="search toolbar-search">
      <Icon name="search" size={15} />
      <input type="text" placeholder={t('commands.searchPlaceholder')} bind:value={search} bind:this={searchInput} />
    </label>
    <button class="btn primary" onclick={openNew} disabled={expanded === NEW}>
      <Icon name="plus" size={14} /> {t('commands.newCommand')}
    </button>
  </div>

  <!-- The deck: ledger list left, docked inspector right. The list never
       disappears — selecting a row (or "new") loads it into the inspector. -->
  <div class="deck {editorDraft ? 'inspecting' : ''}">
    <Card style="padding:6px 0 0" class="deck-list">
      <div class="list">
        {#each rows as c, i (c.name)}
          <CommandRow
            command={c}
            index={i + 1}
            status={rowStatus[c.name] ?? 'idle'}
            unsaved={rowHasDraft(c.name) && expanded !== c.name}
            expanded={expanded === c.name}
            onExpand={() => openEdit(c)}
            onDelete={() => requestDelete(c)}
            toggleSubmit={toggleSubmit(c)}
          />
        {/each}
        {#if rows.length === 0}
          <div class="empty">
            {#if items.length === 0}
              <p class="empty-title">{t('commands.noneYet')}</p>
              <p class="empty-sub">{t('commands.noneYetSub')} <code>!name</code> {t('commands.inChat')}</p>
              <button class="btn primary" onclick={openNew}><Icon name="plus" size={14} /> {t('commands.newCommand')}</button>
            {:else}
              {t('commands.noneMatch')}
            {/if}
          </div>
        {/if}
      </div>
    </Card>

    <!-- Backdrop for the mobile bottom-sheet; Escape (svelte:window below) is
         the keyboard path. A full-screen <button> would hijack the custom
         cursor's interactive morph, hence the div. -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="inspector-backdrop"
      class:open={!!editorDraft}
      role="presentation"
      onclick={closeEditor}
      onkeydown={(e) => { if (e.key === 'Enter') closeEditor(); }}
    ></div>
    <aside class="inspector" class:open={!!editorDraft} aria-label="Command inspector">
      <div class="inspector-head">
        <span class="inspector-tag">
          {#if editorDraft}
            {editorDraft.edit ? t('commands.editing', { name: editorDraft.originalName }) : t('commands.newCommand')}
          {:else}
            {t('commands.inspector')}
          {/if}
        </span>
        {#if editorDraft}
          <button class="mini" type="button" aria-label={t('commands.closeEditor')} onclick={closeEditor}>
            <Icon name="x" size={14} />
          </button>
        {/if}
      </div>
      {#if editorDraft}
        <Scroller fill padding="16px" data-lenis-prevent>
          <CommandEditor bind:draft={editorDraft} {serverErrors} {busy} onCancel={closeEditor} onSubmit={saveSubmit} />
        </Scroller>
      {:else}
        <div class="inspector-idle">
          <span class="idle-glyph"><Icon name="commands" size={18} /></span>
          <p>{t('commands.inspectorIdle')}</p>
          <button class="btn ghost" onclick={openNew}><Icon name="plus" size={13} /> {t('commands.newCommand')}</button>
        </div>
      {/if}
    </aside>
  </div>
</section>

<svelte:window onkeydown={onKey} />

<style>
  .toolbar-search { width: 220px; }

  .keys {
    display: none;
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 11px;
    color: var(--bb-muted);
    align-items: center;
    gap: 6px;
    white-space: nowrap;
  }
  @media (min-width: 1080px) and (pointer: fine) {
    .keys { display: inline-flex; }
  }

  .degraded {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 14px;
    padding: 10px 14px;
    border: 1px solid rgba(176, 90, 70, 0.4);
    border-radius: var(--bb-radius-md, 10px);
    background: rgba(176, 90, 70, 0.08);
    color: #cf8a78;
    font-family: var(--bb-font-body);
    font-size: 13px;
  }

  /* ── the deck: list + docked inspector ── */
  .deck {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  @media (min-width: 1080px) {
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 420px; }
    .deck { grid-template-columns: minmax(0, 1fr) 300px; }
  }

  .list :global(.row-shell:last-child) { border-bottom: none; }

  .inspector {
    position: sticky;
    /* below the call-sign strip; leave clearance for the floating dock */
    top: 62px;
    border: 1px solid var(--rule);
    border-top-color: var(--rule-strong);
    border-radius: var(--bb-radius-lg);
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
    border-radius: var(--bb-radius-sm);
    color: var(--bb-tan-light);
  }
  .inspector-idle p { margin: 0; max-width: 26ch; line-height: 1.5; }

  .inspector-backdrop { display: none; }

  /* Mobile / narrow: the inspector docks as a bottom sheet over the list. */
  @media (max-width: 1079px) {
    .inspector { display: none; }
    .inspector.open {
      display: flex;
      position: fixed;
      left: 0; right: 0; bottom: 0;
      top: auto;
      z-index: 220;
      max-height: 88vh;
      border-radius: var(--bb-radius-lg) var(--bb-radius-lg) 0 0;
      background: var(--bb-bg-1, #111);
      animation: sheet-in var(--bb-dur-base, 320ms) var(--bb-ease-out-expo, cubic-bezier(.16,1,.3,1)) both;
    }
    .inspector-backdrop.open {
      display: block;
      position: fixed; inset: 0; z-index: 219;
      background: rgba(0, 0, 0, 0.55);
    }
    @keyframes sheet-in { from { transform: translateY(100%); } to { transform: translateY(0); } }
  }

  .empty {
    padding: 34px 18px;
    text-align: center;
    color: var(--bb-muted);
    font-family: var(--bb-font-body);
    font-size: 13px;
  }
  .empty-title {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 17px;
    color: var(--bb-white);
    margin: 0 0 6px;
  }
  .empty-sub { margin: 0 0 16px; }
  .empty code {
    font-family: var(--bb-font-mono);
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.1);
    border-radius: 4px;
    padding: 0 5px;
  }

  @media (max-width: 760px) {
    .toolbar { gap: 8px; }
    .toolbar-search { width: 100%; order: 3; }
    .chip-row {
      overflow-x: auto;
      -webkit-overflow-scrolling: touch;
      flex-wrap: nowrap;
      scrollbar-width: none;
    }
    .chip-row::-webkit-scrollbar { display: none; }
  }
</style>
