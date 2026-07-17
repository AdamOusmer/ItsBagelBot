<script lang="ts">
  import { onMount } from 'svelte';
  import { deserialize } from '$app/forms';
  import { replaceState } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    PageHead,
    Scroller,
    PageToolbar,
    AlertBanner,
    DeckList,
    EmptyState,
    InspectorSurface,
    ConfirmDialog,
    toast,
    normName,
    getI18n,
    builtinDef,
    BUILTIN_NAMES,
    validateCommand,
    PERM_LABELS,
    PERMS,
    COMMAND_NAME_MAX,
    COOLDOWN_MAX,
    type CommandView,
    type CommandErrors,
    type Perm
  } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';
  import CommandRow from '$lib/components/commands/CommandRow.svelte';
  import CommandEditor from '$lib/components/commands/CommandEditor.svelte';
  import BuiltinInspector from '$lib/components/commands/BuiltinInspector.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';
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
  // saving -> saved (server ack) -> idle. There is no timer-driven "live"/"synced"
  // claim: the dashboard has no delivery ack, so it must not assert one.
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
    statusTimers.set(name, [setTimeout(() => (rowStatus = { ...rowStatus, [name]: 'idle' }), 3000)]);
  }

  function flagError(name: string) {
    setStatus(name, 'error');
    statusTimers.set(name, [setTimeout(() => (rowStatus = { ...rowStatus, [name]: 'idle' }), 4000)]);
  }

  // --- Filters ---------------------------------------------------------------
  const filters = ['All', 'Active', 'Disabled', 'Built-in', 'Custom'] as const;
  // Internal keys drive the filter logic; only the display label is translated.
  const filterLabel = (f: (typeof filters)[number]) =>
    f === 'Active'
      ? t('commands.filterActive')
      : f === 'Disabled'
        ? t('commands.filterDisabled')
        : f === 'Built-in'
          ? t('commands.filterBuiltin')
          : f === 'Custom'
            ? t('commands.filterCustom')
            : t('commands.filterAll');
  let active = $state<(typeof filters)[number]>('All');
  let search = $state('');

  const rows = $derived(
    items
      .filter((c) => {
        switch (active) {
          case 'Active':
            return c.is_active;
          case 'Disabled':
            return !c.is_active;
          case 'Built-in':
            return !!c.builtin;
          case 'Custom':
            return !c.builtin;
          default:
            return true;
        }
      })
      .filter((c) => {
        const q = search.toLowerCase();
        return (
          c.name.toLowerCase().includes(q) ||
          (c.aliases ?? []).some((a) => a.toLowerCase().includes(q)) ||
          c.response.toLowerCase().includes(q)
        );
      })
      // Built-ins float to the top, then alphabetical within each group.
      .toSorted((a, b) => Number(!!b.builtin) - Number(!!a.builtin) || a.name.localeCompare(b.name))
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
      is_active: c.is_active,
      builtin: c.builtin === true
    };
  }

  // Bumped to remount the editor with a fresh baseline (after save, or on open).
  let editorGen = $state(0);

  // The committed baseline for the open draft (what "saved" looks like), used to
  // tell whether the editor is dirty. Built-ins are read-only (never dirty).
  const committedDraft = $derived.by<CommandDraft | null>(() => {
    if (!editorDraft || editorDraft.builtin) return null;
    if (editorDraft.edit) {
      const cmd = items.find((c) => c.name === editorDraft!.originalName);
      return cmd ? fromView(cmd) : null;
    }
    return blankDraft();
  });
  const isDirty = $derived(
    !!editorDraft && !editorDraft.builtin && committedDraft !== null
      ? JSON.stringify(editorDraft) !== JSON.stringify(committedDraft)
      : false
  );

  // Dirty guard: close / row-switch / new all route through one confirmation
  // rather than silently dropping in-progress edits. The sessionStorage mirror
  // still backs a forced browser reload; a deliberate discard clears it.
  let discardOpen = $state(false);
  let afterDiscard: (() => void) | null = null;
  function guarded(action: () => void) {
    if (isDirty) {
      afterDiscard = action;
      discardOpen = true;
    } else {
      action();
    }
  }
  function confirmDiscard() {
    discardOpen = false;
    if (editorDraft && !editorDraft.builtin) clearDraft(editorDraft.edit ? editorDraft.originalName : '', editorDraft.edit);
    draftVersion++;
    const a = afterDiscard;
    afterDiscard = null;
    a?.();
  }
  function cancelDiscard() {
    discardOpen = false;
    afterDiscard = null;
  }

  function doOpenNew() {
    serverErrors = null;
    // A draft under the "new" key only survives a forced reload; restore quietly.
    editorDraft = loadDraft('', false) ?? blankDraft();
    expanded = NEW;
    editorGen++;
  }
  function doOpenEdit(c: CommandView) {
    serverErrors = null;
    // Built-ins have no editable draft: read-only preview + toggle.
    if (c.builtin) {
      editorDraft = { ...blankDraft(), edit: true, name: c.name, originalName: c.name, is_active: c.is_active, builtin: true };
      expanded = c.name;
      editorGen++;
      return;
    }
    // loadDraft only returns something after a forced reload; restore quietly.
    editorDraft = loadDraft(c.name, true) ?? fromView(c);
    expanded = c.name;
    editorGen++;
  }
  // Unguarded close for the delete path (the command is already gone).
  function doCloseEditor() {
    expanded = null;
    editorDraft = null;
    serverErrors = null;
    draftVersion++;
  }

  function openNew() {
    guarded(doOpenNew);
  }

  // --- Deep-link compose (marketing command builder) ---------------------------
  // The public command builder links here as /commands?compose=1&name=…&response=…
  // A valid draft opens a confirmation modal (summary + the same chat rehearsal
  // as the editor) and one press creates the command — no inspector round-trip.
  // A draft the validator rejects falls back to the pre-filled editor so the
  // visitor can fix it. The path (with query) survives the login round-trip via
  // safeNextPath, same as /billing?subscribe=1. Runs once on mount; the params
  // are then stripped so a refresh can't re-run a stale compose.
  let composeDraft = $state<CommandDraft | null>(null);
  let composeBusy = $state(false);
  const composeReplaces = $derived(
    composeDraft !== null && items.some((c) => !c.builtin && c.name === composeDraft!.name)
  );

  onMount(() => {
    const url = new URL(window.location.href);
    if (url.searchParams.get('compose') !== '1') return;

    const perm = url.searchParams.get('perm') ?? '';
    const cooldown = Math.floor(Number(url.searchParams.get('cooldown')) || 0);
    const aliases = (url.searchParams.get('aliases') ?? '').split(',').map(normName).filter(Boolean);

    const draft: CommandDraft = {
      ...blankDraft(),
      name: normName(url.searchParams.get('name') ?? '').slice(0, COMMAND_NAME_MAX),
      // The Go validator caps aliases at 25; drop the excess instead of failing.
      aliases: [...new Set(aliases)].slice(0, 25),
      // 2504 is the response column's MaxLen; validateCommand owns line rules.
      response: (url.searchParams.get('response') ?? '').slice(0, 2504),
      perm: (PERMS as readonly string[]).includes(perm) ? (perm as Perm) : 'everyone',
      cooldown: Math.min(Math.max(cooldown, 0), COOLDOWN_MAX)
    };

    const problems = validateCommand({
      name: draft.name,
      aliases: draft.aliases,
      response: draft.response,
      cooldown: draft.cooldown,
      allowedUserId: ''
    });
    if (BUILTIN_NAMES.has(draft.name)) {
      problems.name = t('commands.errBuiltinName');
    }
    if (Object.keys(problems).length === 0) {
      composeDraft = draft;
    } else {
      // Broken deep link: hand it to the editor with the fields to fix.
      serverErrors = problems;
      editorDraft = draft;
      expanded = NEW;
      editorGen++;
    }

    for (const key of ['compose', 'name', 'response', 'perm', 'cooldown', 'aliases', 'lang']) {
      url.searchParams.delete(key);
    }
    // Deferred: the router hasn't claimed the initial history entry yet when
    // onMount runs, and a same-tick replaceState is dropped.
    setTimeout(() => replaceState(url, {}), 0);
  });

  function composeCancel() {
    if (composeBusy) return;
    composeDraft = null;
  }

  async function composeConfirm() {
    const d = composeDraft;
    if (!d || composeBusy) return;
    composeBusy = true;
    const replacing = composeReplaces;
    const view: CommandView = {
      name: d.name,
      aliases: d.aliases,
      response: d.response,
      is_active: true,
      stream_online_only: false,
      perm: d.perm,
      cooldown: d.cooldown,
      allowed_user_id: ''
    };
    const body = formDataFor(view);
    if (replacing) {
      // The modal warned "replace" — save as an edit so the server reports
      // (and audits) an update, not a create.
      body.set('edit', '1');
      body.set('original_name', d.name);
    }
    setStatus(d.name, 'saving');
    const payload = await postAction('save', body);
    composeBusy = false;
    composeDraft = null;
    if (payload?.ok) {
      applyResult(payload); // reconciles the row and toasts created/updated
      // The write can land while the read-back fails (empty commands[]);
      // reconcile from the draft so the toasted command is actually visible.
      if (!items.some((c) => c.name === d.name)) {
        items = [...items, view];
      }
      ackSaved(d.name);
      return;
    }
    flagError(d.name);
    // Never drop the composed work: reopen it in the editor, with the
    // server's field complaints when it sent any.
    serverErrors = payload?.errors ?? null;
    editorDraft = d;
    expanded = NEW;
    editorGen++;
    if (!payload?.errors) toast('err', payload?.error ?? t('commands.toastSaveFailed'));
  }
  function openEdit(c: CommandView) {
    if (expanded === c.name) {
      closeEditor();
      return;
    }
    guarded(() => doOpenEdit(c));
  }
  function closeEditor() {
    guarded(doCloseEditor);
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
    // The selection at submit time. The row-keyed list reconciliation (applyResult)
    // is order-independent, but the editor re-seed and inline serverErrors target
    // whatever is open — so a late response must only touch the editor if the user
    // is still on the command it was submitted for.
    const submittedExpanded = expanded;

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

      // Did the user leave the command this response was submitted for?
      const stillOpen = expanded === submittedExpanded;

      if (result.type === 'success' && payload?.ok) {
        applyResult({ ...payload, silent: true });
        clearDraft(d.edit ? d.originalName : '', d.edit);
        ackSaved(key);
        // Save keeps the inspector open on the saved command (renamed or not),
        // re-seeded so it reads clean — but only if it is still the open editor;
        // otherwise the list is reconciled silently and the current selection is
        // left untouched.
        if (stillOpen) {
          const saved = items.find((c) => c.name === key);
          if (saved) {
            editorDraft = fromView(saved);
            expanded = key;
            serverErrors = null;
            editorGen++;
          } else {
            doCloseEditor();
          }
        }
        return;
      }

      // Rollback the affected rows; keep the editor open with the draft intact.
      items = [...items.filter((c) => c.name !== key && c.name !== orig), ...prevRows];
      flagError(orig ?? key);
      // Attach validation errors only to the editor they belong to.
      if (stillOpen) serverErrors = payload?.errors ?? null;
      // Field-level validation shows inline; anything else (RPC failure, missing
      // payload) falls back to the localized generic toast so the failure is
      // never silent. The server logs the real reason.
      if (!payload?.errors) toast('err', payload?.error ?? t('commands.toastSaveFailed'));
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

  // --- Built-in reply save (optimistic response swap) ------------------------
  // Editable built-ins (e.g. clip) persist their reply template to the modules
  // service. Optimistically swap the row's response to the typed value, then let
  // the server's rebuilt row reconcile (it normalizes a blank reply back to the
  // default template).
  const replySubmit =
    (c: CommandView): SubmitFunction =>
    ({ formData }) => {
      const next = String(formData.get('reply') ?? '');
      const before = { ...c };
      items = items.map((x) => (x.name === c.name ? { ...x, response: next } : x));
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
          toast('err', payload?.error ?? t('commands.toastSaveFailed'));
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
    if (expanded === c.name) doCloseEditor();
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

  // The command currently loaded in the inspector (built-in path reads it live).
  const selectedCmd = $derived(
    expanded && expanded !== NEW ? items.find((c) => c.name === expanded) : undefined
  );

  // The open editor's footer status. Maps the legacy 'live' row state (no longer
  // produced) to 'saved' so it fits the footer's state set.
  type FooterStatus = 'idle' | 'saving' | 'saved' | 'error' | 'conflict';
  function footerStatus(): FooterStatus {
    if (busy) return 'saving';
    const s = rowStatus[expanded ?? ''] ?? 'idle';
    return s === 'live' ? 'saved' : (s as FooterStatus);
  }

  // --- Keyboard control: "/" jumps to search, "n" starts a new command,
  // Escape closes the inspector. Ignored while typing in any field. ---
  let searchInput = $state<HTMLInputElement | null>(null);

  function isTyping(e: KeyboardEvent): boolean {
    const t = e.target as HTMLElement | null;
    return !!t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable);
  }

  // Escape is owned by the InspectorSurface (it yields to the discard dialog);
  // the page only handles the search / new shortcuts.
  function onKey(e: KeyboardEvent) {
    if (composeDraft !== null || discardOpen) return;
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
    <AlertBanner>{t('commands.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      <div class="chip-row">
        {#each filters as f}
          <button class="chip {active === f ? 'on' : ''}" onclick={() => (active = f)}>{filterLabel(f)}</button>
        {/each}
      </div>
    {/snippet}
    {#snippet trail()}
      <span class="keys" aria-hidden="true"><kbd class="hint">/</kbd> {t('commands.keysSearch')} <kbd class="hint">N</kbd> {t('commands.keysNew')}</span>
      <label class="search toolbar-search">
        <Icon name="search" size={15} />
        <input type="text" placeholder={t('commands.searchPlaceholder')} bind:value={search} bind:this={searchInput} />
      </label>
      <button class="btn primary" onclick={openNew} disabled={expanded === NEW}>
        <Icon name="plus" size={14} /> {t('commands.newCommand')}
      </button>
    {/snippet}
  </PageToolbar>

  <!-- The deck: ledger list left, docked inspector right. The list never
       disappears — selecting a row (or "new") loads it into the inspector. -->
  <div class="deck {editorDraft ? 'inspecting' : ''}">
    <DeckList>
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
          {#if items.length === 0}
            <EmptyState
              icon="commands"
              title={t('commands.noneYet')}
              body={`${t('commands.noneYetSub')} !name ${t('commands.inChat')}`}
            >
              <button class="btn primary" onclick={openNew}><Icon name="plus" size={14} /> {t('commands.newCommand')}</button>
            </EmptyState>
          {:else}
            <EmptyState title={t('commands.noneMatch')} />
          {/if}
        {/if}
      </div>
    </DeckList>

    {#if editorDraft}
      <InspectorSurface
        open
        title={editorDraft.builtin
          ? `!${editorDraft.name}`
          : editorDraft.edit
            ? t('commands.editing', { name: editorDraft.originalName })
            : t('commands.newCommand')}
        controls="command-editor"
        closeLabel={t('commands.closeEditor')}
        onClose={closeEditor}
      >
        {#if editorDraft.builtin && selectedCmd}
          {@const def = builtinDef(selectedCmd.name)}
          {#if def}
            <Scroller fill padding="16px" data-lenis-prevent>
              <BuiltinInspector
                command={selectedCmd}
                {def}
                toggleSubmit={toggleSubmit(selectedCmd)}
                replySubmit={replySubmit(selectedCmd)}
                {busy}
              />
            </Scroller>
          {/if}
        {:else}
          <!-- Keyed on selection + generation so switching rows (or a save)
               mounts a FRESH editor: it snapshots its draft key + initial value
               at mount, so reusing one instance across commands would freeze
               those to the first row. -->
          {#key expanded + '#' + editorGen}
            <CommandEditor
              bind:draft={editorDraft}
              {serverErrors}
              status={footerStatus()}
              dirty={isDirty}
              canSave={isDirty && editorDraft.name.trim().length > 0}
              onCancel={closeEditor}
              onSubmit={saveSubmit}
            />
          {/key}
        {/if}
      </InspectorSurface>
    {/if}
  </div>
</section>

<ConfirmDialog
  open={composeDraft !== null}
  title={t('commands.composeTitle', { name: composeDraft?.name ?? '' })}
  body={composeReplaces ? t('commands.composeReplace', { name: composeDraft?.name ?? '' }) : undefined}
  confirmLabel={composeReplaces ? t('commandEditor.saveChanges') : t('commandEditor.create')}
  cancelLabel={t('common.cancel')}
  busy={composeBusy}
  onConfirm={composeConfirm}
  onCancel={composeCancel}
>
  {#if composeDraft}
    <div class="compose-meta">
      <span>{PERM_LABELS[composeDraft.perm]}</span>
      <span>{t('commandRow.cooldown')} {composeDraft.cooldown}s</span>
      {#if composeDraft.aliases.length}
        <span>{t('commandRow.also', { aliases: composeDraft.aliases.map((a) => '!' + a).join(' ') })}</span>
      {/if}
    </div>
    <ChatPreview name={composeDraft.name} response={composeDraft.response} />
  {/if}
</ConfirmDialog>

<ConfirmDialog
  open={discardOpen}
  title={t('commands.discardTitle')}
  body={t('commands.discardBody')}
  confirmLabel={t('commands.discard')}
  cancelLabel={t('commands.keepEditing')}
  danger
  onCancel={cancelDiscard}
  onConfirm={confirmDiscard}
/>

<svelte:window onkeydown={onKey} />

<style>
  .compose-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 14px;
    margin-bottom: 4px;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.04em;
    color: var(--bb-muted);
  }

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

  /* the deck: full-width list until a selection opens the docked inspector. */
  .deck {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  @media (min-width: 1080px) {
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 420px; }
  }

  .list :global(.row-shell:last-child) { border-bottom: none; }

  @media (max-width: 760px) {
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
