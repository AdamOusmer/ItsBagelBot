<script lang="ts">
  import { formatCounterValue } from '@bagel/kit/validation';
  import { Kbd, SearchInput } from '@bagel/kit';
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount, untrack } from 'svelte';
  import { deserialize } from '$app/forms';
  import { replaceState } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import {
    Icon,
    PageHead,
    Scroller,
    PageToolbar,
    AlertBanner,
    ButtonLink,
    DeckList,
    EmptyState,
    InspectorSurface,
    ConfirmDialog,
    toast,
    normName,
    getI18n,
    tPerm,
    toastFailure,
    builtinDef,
    BUILTIN_NAMES,
    validateCommand,
    persistCommandActive,
    overlayLiveActive,
    commandContentSnapshot,
    usesCount,
    compareUses,
    PERMS,
    COMMAND_NAME_MAX,
    COOLDOWN_MAX,
    type CommandView,
    type CommandErrors,
    type Perm,
    SegmentedControl
  } from '@bagel/kit';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import CommandRow from '$lib/components/commands/CommandRow.svelte';
  import CommandEditor from '$lib/components/commands/CommandEditor.svelte';
  import type { SourceDef } from '$lib/components/commands/fetches/FetchSourcePicker.svelte';
  import BuiltinInspector from '$lib/components/commands/BuiltinInspector.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';
  import { loadDraft, clearDraft, hasDraft, type CommandDraft } from '$lib/components/commands/drafts';

  let { data } = $props();

  const { t } = getI18n();
  const failed = toastFailure(toast, t);

  // svelte-ignore state_referenced_locally
  let items = $state<CommandView[]>(data.commands ?? []);

  // svelte-ignore state_referenced_locally
  let seed = data.commands;
  $effect(() => {
    if (data.commands !== seed) {
      seed = data.commands;
      items = data.commands ?? [];
    }
  });

  // svelte-ignore state_referenced_locally
  let fetchDefs = $state<SourceDef[]>(data.defs ?? []);
  // svelte-ignore state_referenced_locally
  let defsSeed = data.defs;
  $effect(() => {
    if (data.defs !== defsSeed) {
      defsSeed = data.defs;
      fetchDefs = data.defs ?? [];
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

  const filters = ['All', 'Active', 'Disabled', 'Built-in', 'Custom'] as const;
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
  const filterOptions = $derived(filters.map(filterLabel));
  let activeLabel = $state(filterLabel('All'));
  const active = $derived(filters.find((f) => filterLabel(f) === activeLabel) ?? 'All');
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
      .toSorted((a, b) => compareUses(b, a) || a.name.localeCompare(b.name))
  );

  const groups = $derived(
    [
      {
        key: 'custom',
        label: t('commands.groupYours'),
        note: t('commands.groupYoursNote'),
        rows: rows.filter((c) => !c.builtin)
      },
      {
        key: 'builtin',
        label: t('commands.groupBuiltin'),
        note: t('commands.groupBuiltinNote'),
        rows: rows.filter((c) => c.builtin)
      }
    ].filter((g) => g.rows.length > 0)
  );

  const usesMax = $derived(rows.reduce((max, c) => usesCount(c) > max ? usesCount(c) : max, 1n));

  const fires = $derived(items.reduce((n, c) => n + usesCount(c), 0n));
  const busiest = $derived(
    items.length === 0
      ? null
      : items.reduce((top, c) => (usesCount(c) > usesCount(top) ? c : top))
  );

  const NEW = '__new__';
  let expanded = $state<string | null>(null);
  let editorDraft = $state<CommandDraft | null>(null);
  let serverErrors = $state<CommandErrors | null>(null);
  let busy = $state(false);
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
      bump_counter: '',
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
      bump_counter: c.bump_counter ?? '',
      stream_online_only: c.stream_online_only === true,
      is_active: c.is_active,
      builtin: c.builtin === true
    };
  }

  let editorGen = $state(0);

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
      ? editorDraft.edit
        ? commandContentSnapshot(editorDraft) !== commandContentSnapshot(committedDraft)
        : JSON.stringify(editorDraft) !== JSON.stringify(committedDraft)
      : false
  );

  const discard = createDiscardGuard(
    () => isDirty,
    () => {
      if (editorDraft && !editorDraft.builtin) clearDraft(editorDraft.edit ? editorDraft.originalName : '', editorDraft.edit);
      draftVersion++;
    }
  );

  function doOpenNew(name = '') {
    serverErrors = null;
    const draft = loadDraft('', false) ?? blankDraft();
    editorDraft = name ? { ...draft, name } : draft;
    expanded = NEW;
    editorGen++;
  }
  function doOpenEdit(c: CommandView) {
    serverErrors = null;
    if (c.builtin) {
      editorDraft = { ...blankDraft(), edit: true, name: c.name, originalName: c.name, is_active: c.is_active, builtin: true };
      expanded = c.name;
      editorGen++;
      return;
    }
    editorDraft = overlayLiveActive(loadDraft(c.name, true) ?? fromView(c), c.is_active);
    expanded = c.name;
    editorGen++;
  }

  $effect(() => {
    const d = editorDraft;
    const live =
      d && d.edit && !d.builtin ? items.find((c) => c.name === d.originalName) : undefined;
    if (!d || !live || d.is_active === live.is_active) return;
    untrack(() => {
      editorDraft = overlayLiveActive(d, live.is_active);
    });
  });
  function doCloseEditor() {
    expanded = null;
    editorDraft = null;
    serverErrors = null;
    draftVersion++;
  }

  function openNew() {
    discard.guard(() => doOpenNew());
  }

  const typedName = $derived(normName(search));
  const canCreateTyped = $derived(
    typedName.length > 1 &&
      typedName.length <= COMMAND_NAME_MAX &&
      !items.some((c) => c.name === typedName) &&
      !BUILTIN_NAMES.has(typedName)
  );
  function createTyped() {
    const name = typedName;
    discard.guard(() => doOpenNew(name));
  }

  const COMMAND_ALIASES_MAX = 25;
  const RESPONSE_COLUMN_MAX_CHARS = 2504;
  let composeDraft = $state<CommandDraft | null>(null);
  let composeBusy = $state(false);
  const matchingCustom = (name: string) => items.find((c) => !c.builtin && c.name === name);
  const composeReplaces = $derived(composeDraft !== null && !!matchingCustom(composeDraft.name));

  onMount(() => {
    const url = new URL(window.location.href);
    if (url.searchParams.get('compose') !== '1') return;

    const perm = url.searchParams.get('perm') ?? '';
    const cooldown = Math.floor(Number(url.searchParams.get('cooldown')) || 0);
    const aliases = (url.searchParams.get('aliases') ?? '').split(',').map(normName).filter(Boolean);

    const draft: CommandDraft = {
      ...blankDraft(),
      name: normName(url.searchParams.get('name') ?? '').slice(0, COMMAND_NAME_MAX),
      aliases: [...new Set(aliases)].slice(0, COMMAND_ALIASES_MAX),
      response: (url.searchParams.get('response') ?? '').slice(0, RESPONSE_COLUMN_MAX_CHARS),
      perm: (PERMS as readonly string[]).includes(perm) ? (perm as Perm) : 'everyone',
      cooldown: Math.min(Math.max(cooldown, 0), COOLDOWN_MAX)
    };

    const problems = validateCommand({
      name: draft.name,
      aliases: draft.aliases,
      response: draft.response,
      cooldown: draft.cooldown,
      allowedUserId: '',
      bumpCounter: ''
    });
    if (BUILTIN_NAMES.has(draft.name)) {
      problems.name = t('commands.errBuiltinName');
    }
    if (Object.keys(problems).length === 0) {
      composeDraft = draft;
    } else {
      serverErrors = problems;
      editorDraft = draft;
      expanded = NEW;
      editorGen++;
    }

    for (const key of ['compose', 'name', 'response', 'perm', 'cooldown', 'aliases', 'lang']) {
      url.searchParams.delete(key);
    }
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
    const existing = matchingCustom(d.name);
    const view: CommandView = {
      name: d.name,
      aliases: d.aliases,
      response: d.response,
      is_active: existing ? existing.is_active : true,
      stream_online_only: false,
      perm: d.perm,
      cooldown: d.cooldown,
      allowed_user_id: ''
    };
    const body = formDataFor(view);
    if (existing) {
      body.set('edit', '1');
      body.set('original_name', d.name);
    }
    setStatus(d.name, 'saving');
    const payload = await postAction('save', body);
    composeBusy = false;
    composeDraft = null;
    if (payload?.ok) {
      applyResult(payload);
      if (!items.some((c) => c.name === d.name)) {
        items = [...items, view];
      }
      ackSaved(d.name);
      return;
    }
    flagError(d.name);
    serverErrors = payload?.errors ?? null;
    editorDraft = d;
    expanded = NEW;
    editorGen++;
    if (!payload?.errors) failed(payload, 'commands.toastSaveFailed');
  }
  function openEdit(c: CommandView) {
    if (expanded === c.name) {
      closeEditor();
      return;
    }
    discard.guard(() => doOpenEdit(c));
  }
  function closeEditor() {
    discard.guard(doCloseEditor);
  }

  const rowHasDraft = (name: string) => {
    void draftVersion;
    return hasDraft(name);
  };

  const saveSubmit: SubmitFunction = ({ formData }) => {
    const d = editorDraft;
    if (!d) return;
    const key = normName(d.name);
    const orig = d.edit ? normName(d.originalName) : undefined;
    const submittedExpanded = expanded;

    const live = items.find((c) => c.name === (orig ?? key));
    const isActive = persistCommandActive(d.edit, d.is_active, live?.is_active);
    formData.set('is_active', isActive ? 'on' : '');

    const prevRows = items.filter((c) => c.name === key || c.name === orig);
    const optimistic: CommandView = {
      name: key,
      aliases: d.aliases.map(normName).filter(Boolean),
      response: d.response,
      is_active: isActive,
      stream_online_only: d.stream_online_only,
      perm: d.perm,
      cooldown: Math.floor(Number(d.cooldown) || 0),
      allowed_user_id: d.allowed_user_id.replace(/\D/g, ''),
      bump_counter: d.bump_counter,
      uses: live?.uses
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

      const stillOpen = expanded === submittedExpanded;

      if (result.type === 'success' && payload?.ok) {
        applyResult({ ...payload, silent: true });
        clearDraft(d.edit ? d.originalName : '', d.edit);
        ackSaved(key);
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

      items = [...items.filter((c) => c.name !== key && c.name !== orig), ...prevRows];
      flagError(orig ?? key);
      if (stillOpen) serverErrors = payload?.errors ?? null;
      if (!payload?.errors) failed(payload, 'commands.toastSaveFailed');
    };
  };

  function settleToggle(name: string, before: CommandView, payload: ActionResult | null | undefined, ok: boolean) {
    if (ok && payload?.ok) {
      applyResult(payload);
      ackSaved(name);
      return;
    }
    items = items.map((x) => (x.name === name ? before : x));
    flagError(name);
    toast('err', payload?.error ?? t('commands.toastToggleFailed'));
  }

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
        settleToggle(c.name, before, payload, result.type === 'success');
      };
    };

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
          applyResult(payload);
          ackSaved(c.name);
        } else {
          items = items.map((x) => (x.name === c.name ? before : x));
          flagError(c.name);
          toast('err', payload?.error ?? t('commands.toastSaveFailed'));
        }
      };
    };

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
    body.set('bump_counter', c.bump_counter ?? '');
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

  const selectedCmd = $derived(
    expanded && expanded !== NEW ? items.find((c) => c.name === expanded) : undefined
  );

  function setSelectedActive(next: boolean) {
    const c = selectedCmd;
    if (!c || c.builtin || c.is_active === next) return;
    const before = { ...c };
    items = items.map((x) => (x.name === c.name ? { ...x, is_active: next } : x));
    setStatus(c.name, 'saving');
    void postAction('toggle', formDataFor({ ...c, is_active: next })).then((payload) => {
      settleToggle(c.name, before, payload, payload?.ok === true);
    });
  }

  type FooterStatus = 'idle' | 'saving' | 'saved' | 'error' | 'conflict';
  function footerStatus(): FooterStatus {
    if (busy) return 'saving';
    const s = rowStatus[expanded ?? ''] ?? 'idle';
    return s === 'live' ? 'saved' : (s as FooterStatus);
  }

  let searchInput = $state<HTMLInputElement | undefined>(undefined);

  function isTyping(e: KeyboardEvent): boolean {
    const t = e.target as HTMLElement | null;
    return !!t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable);
  }

  function onKey(e: KeyboardEvent) {
    if (composeDraft !== null || discard.open) return;
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
  <PageHead eyebrow={t('commands.eyebrow')} description={t('commands.description')}>
    {t('commands.titlePre')}<em>{t('commands.titleEm')}</em>
    {#snippet trail()}
      <dl class="deck-stats">
        <div class="ds-cell">
          <dt>{t('commands.statActive')}</dt>
          <dd><span class="big">{activeCount}</span><span class="of">/{items.length}</span></dd>
        </div>
        <div class="ds-rule" aria-hidden="true"></div>
        <div class="ds-cell">
          <dt>{t('commands.statFires')}</dt>
          <dd><span class="big">{formatCounterValue(fires.toString())}</span></dd>
        </div>
        <div class="ds-rule" aria-hidden="true"></div>
        <div class="ds-cell">
          <dt>{t('commands.statBusiest')}</dt>
          <dd><span class="mono">{busiest ? `!${busiest.name}` : t('commands.statNone')}</span></dd>
        </div>
      </dl>
    {/snippet}
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('commands.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      <SegmentedControl
        options={filterOptions}
        bind:value={activeLabel}
        label={t('commands.filterLabel')}
      />
    {/snippet}
    {#snippet trail()}
      <span class="keys" aria-hidden="true"><Kbd>/</Kbd> {t('commands.keysSearch')} <Kbd>N</Kbd> {t('commands.keysNew')}</span>
      <div class="toolbar-search">
        <SearchInput placeholder={t('commands.searchPlaceholder')} clearLabel={t('quotes.searchClear')}
          aria-label={t('commands.searchPlaceholder')} bind:value={search} bind:element={searchInput} fill />
      </div>
      <button class="bb-btn bb-btn--primary" onclick={openNew} disabled={expanded === NEW}>
        {t('commands.newCommand')}
      </button>
    {/snippet}
  </PageToolbar>

  {#if canCreateTyped}
    <button class="create-hint" type="button" onclick={createTyped} aria-label={t('commands.createHintAria', { name: typedName })}>
      <span class="ch-name">!{typedName}</span>
      <span class="ch-body">{t('commands.createHint')}</span>
      <span class="ch-grow"></span>
      <span class="ch-cta">{t('commands.createHintCta')}</span>
    </button>
  {/if}

  <div class="deck {editorDraft ? 'inspecting' : ''}">
    <DeckList>
      <div class="list">
        {#each groups as g (g.key)}
          <div class="group">
            <div class="group-head">
              <span class="g-label">{g.label}</span>
              <span class="g-count">{g.rows.length}</span>
              <span class="g-rule" aria-hidden="true"></span>
              <span class="g-note">{g.note}</span>
            </div>
            {#each g.rows as c, i (c.name)}
              <CommandRow
                command={c}
                index={i + 1}
                {usesMax}
                status={rowStatus[c.name] ?? 'idle'}
                unsaved={rowHasDraft(c.name) && expanded !== c.name}
                expanded={expanded === c.name}
                onExpand={() => openEdit(c)}
                onDelete={() => requestDelete(c)}
                toggleSubmit={toggleSubmit(c)}
              />
            {/each}
          </div>
        {/each}
        {#if rows.length === 0}
          {#if items.length === 0}
            <EmptyState
              title={t('commands.noneYet')}
              body={`${t('commands.noneYetSub')} !name ${t('commands.inChat')}`}
            >
              <button class="bb-btn bb-btn--primary" onclick={openNew}>{t('commands.newCommand')}</button>
            </EmptyState>
          {:else}
            <EmptyState title={t('commands.noneMatch')} body={t('commands.noneMatchSub')} />
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
            <Scroller fill padding="16px" smooth>
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
          {#key expanded + '#' + editorGen}
            <CommandEditor
              bind:draft={editorDraft}
              {serverErrors}
              status={footerStatus()}
              dirty={isDirty}
              canSave={editorDraft.edit ? isDirty : true}
              {fetchDefs}
              fetchKeys={data.keys ?? []}
              onFetchDefsChanged={(next) => (fetchDefs = next)}
              onCancel={closeEditor}
              onSubmit={saveSubmit}
              liveActive={selectedCmd?.is_active ?? editorDraft.is_active}
              onToggleActive={setSelectedActive}
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
      <span>{tPerm(t, composeDraft.perm)}</span>
      <span>{t('commandRow.cooldown')} {composeDraft.cooldown}s</span>
      {#if composeDraft.aliases.length}
        <span>{t('commandRow.also', { aliases: composeDraft.aliases.map((a) => '!' + a).join(' ') })}</span>
      {/if}
    </div>
    <ChatPreview name={composeDraft.name} response={composeDraft.response} />
  {/if}
</ConfirmDialog>

<ConfirmDialog
  open={discard.open}
  title={t('commands.discardTitle')}
  body={t('commands.discardBody')}
  confirmLabel={t('commands.discard')}
  cancelLabel={t('commands.keepEditing')}
  danger
  onCancel={discard.cancel}
  onConfirm={discard.confirm}
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

  .deck-stats {
    display: flex;
    gap: 26px;
    margin: 0;
    padding: 14px 0;
    border-top: 1px solid var(--bb-border);
    border-bottom: 1px solid var(--bb-border);
  }
  .ds-cell { white-space: nowrap; }
  .ds-rule { width: 1px; align-self: stretch; background: var(--bb-border); }
  .deck-stats dt {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-muted);
    margin-bottom: 6px;
  }
  .deck-stats dd { margin: 0; display: flex; align-items: baseline; gap: 4px; }
  .deck-stats .big {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: 26px;
    line-height: 1;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    font-variant-numeric: tabular-nums;
  }
  .deck-stats .of { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-muted); }
  .deck-stats .mono { font-family: var(--bb-font-mono); font-size: 15px; color: var(--bb-tan-light); line-height: 1.7; }

  .create-hint {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
    padding: 14px 16px;
    margin-bottom: 14px;
    border: 1px dashed rgba(82, 183, 136, 0.4);
    border-radius: var(--bb-radius-sm);
    background: rgba(82, 183, 136, 0.06);
    cursor: pointer;
    text-align: left;
  }
  .create-hint:hover { border-color: var(--bb-green-glow); background: rgba(82, 183, 136, 0.1); }
  .create-hint:focus-visible { outline: 2px solid var(--bb-green-glow); outline-offset: 2px; }
  .ch-name { font-family: var(--bb-font-mono); font-size: 13.5px; color: var(--bb-green-glow); }
  .ch-body { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-muted); }
  .ch-grow { flex: 1; }
  .ch-cta {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-green-glow);
  }

  .group + .group { margin-top: 26px; }
  .group-head {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 0 14px 9px;
  }
  .g-label {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--bb-tan);
  }
  .g-count { font-family: var(--bb-font-mono); font-size: 10.5px; color: var(--bb-muted); }
  .g-rule { flex: 1; height: 1px; background: var(--bb-border); }
  .g-note {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
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

  .deck {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  @media (min-width: 1080px) {
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 420px; }
  }

  .group:last-child :global(.row-wrap:last-child .row-shell) { border-bottom: none; }

  @media (max-width: 760px) {
    .deck-stats { gap: 16px; }
    .deck-stats .big { font-size: 20px; }
    .toolbar-search { width: 100%; order: 3; }
  }
</style>
