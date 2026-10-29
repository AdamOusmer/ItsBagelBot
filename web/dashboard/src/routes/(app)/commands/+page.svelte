<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { formatCounterValue } from '@bagel/kit/validation';
  import { onMount, tick, untrack } from 'svelte';
  import { postAction as postActionShared, reseedOnChange } from '#lib/client-actions.svelte';
  import { invalidate, replaceState } from '$app/navigation';
  import { expectEcho } from '#lib/local-write';
  import type { SubmitFunction } from '$app/forms';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import { isShortcut } from '@bagel/ui/lib/hotkeys';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import Kbd from '@bagel/ui/svelte/Kbd.svelte';
  import Divider from '@bagel/ui/svelte/Divider.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import {
    normName,
    getI18n,
    tPerm,
    toastFailure,
    builtinDef,
    BUILTIN_NAMES,
    validateCommand,
    persistCommandActive,
    overlayLiveActive,
    usesCount,
    PERMS,
    COMMAND_NAME_MAX,
    actionPayload,
    type CommandView,
    type CommandErrors,
    type Perm
  } from '@bagel/kit';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import CommandRow from '#lib/components/commands/CommandRow.svelte';
  import type { SourceDef } from '#lib/components/commands/fetches/FetchSourcePicker.svelte';
  import BulkBar from '#lib/components/commands/BulkBar.svelte';
  import PublicPageChip from '#lib/components/commands/PublicPageChip.svelte';
  import StarterCommands from '#lib/components/commands/StarterCommands.svelte';
  import { rovingList } from '#lib/components/commands/roving';
  import { COMPOSE_PARAMS, parseComposeLink, type ComposeLink } from '#lib/components/commands/compose-link';
  import { STATE_FILTERS, hasCreatedAt, listCommands, stateCounts, type PermFilter, type SortKey, type StateFilter } from '#lib/components/commands/list-model';
  import { saveFailureErrors, isUnavailable, type SaveFailure } from '#lib/components/commands/save-errors';
  import type { Starter } from '#lib/components/commands/starters';
  import { loadDraft, clearDraft, draftedNames, draftsMatch, discardLegacyDrafts, draftRef, type CommandDraft } from '#lib/components/commands/drafts';
  import { warmOnIntent } from '#lib/warm-on-intent';
  const loadCommandEditor = () => import('#lib/components/commands/CommandEditor.svelte');
  const loadBuiltinInspector = () => import('#lib/components/commands/BuiltinInspector.svelte');
  const loadChatPreview = () => import('#lib/components/commands/ChatPreview.svelte');

  let { data } = $props();

  const { t, locale } = getI18n();
  const failed = toastFailure(toast);

  // svelte-ignore state_referenced_locally
  let items = $state.raw<CommandView[]>(data.commands ?? []);

  // svelte-ignore state_referenced_locally
  let fetchDefs = $state<SourceDef[]>(data.defs ?? []);

  reseedOnChange(() => data.commands, (next) => (items = next ?? []));
  reseedOnChange(() => data.defs, (next) => (fetchDefs = next ?? []));

  type ActionResult = SaveFailure & {
    ok: boolean;
    action?: 'created' | 'updated' | 'deleted';
    name?: string;
    original?: string;
    silent?: boolean;
    error?: string;
    restored?: boolean;
    commands?: CommandView[];
    op?: 'enable' | 'disable' | 'delete';
    results?: { name: string; ok: boolean }[];
  };

  function applyResult(d: ActionResult) {
    if (!d.ok) {
      if (d.error) toast('danger', d.error);
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
      toast('success', t(key, { name: d.name ?? '' }));
    }
  }

  let rowStatus = $state<Record<string, SaveState>>({});
  const statusTimers = new Map<string, ReturnType<typeof setTimeout>[]>();

  function clearTimers(name: string) {
    for (const timer of statusTimers.get(name) ?? []) clearTimeout(timer);
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

  const STATE_LABEL_KEYS = {
    all: 'commands.filterAll',
    active: 'commands.filterActive',
    disabled: 'commands.filterDisabled',
    builtin: 'commands.filterBuiltin',
    custom: 'commands.filterCustom'
  } as const;

  let stateFilter = $state<StateFilter>('all');
  let permFilter = $state<string>('all');
  let sortKey = $state<string>('uses');
  let search = $state('');
  let lingering = $state<ReadonlySet<string>>(new Set());

  $effect(() => {
    void [stateFilter, permFilter, search];
    lingering = new Set();
  });

  const numberFormat = $derived(new Intl.NumberFormat(locale));
  const counts = $derived(stateCounts(items, { perm: permFilter as PermFilter, search }));
  const stateOptions = $derived(
    STATE_FILTERS.map((f) =>
      t('commands.filterWithCount', { label: t(STATE_LABEL_KEYS[f]), count: numberFormat.format(counts[f]) })
    )
  );
  const stateValue = {
    get: () => stateOptions[STATE_FILTERS.indexOf(stateFilter)],
    set: (label: string) => (stateFilter = STATE_FILTERS[stateOptions.indexOf(label)] ?? 'all')
  };
  const sortOptions = $derived([
    { value: 'uses', label: t('commands.sortUses') },
    { value: 'name', label: t('commands.sortName') },
    ...(hasCreatedAt(items) ? [{ value: 'recent', label: t('commands.sortRecent') }] : [])
  ]);
  const permOptions = $derived([
    { value: 'all', label: t('commands.permAll') },
    ...PERMS.map((p) => ({ value: p, label: tPerm(t, p) }))
  ]);

  const rows = $derived(
    listCommands(items, { state: stateFilter, perm: permFilter as PermFilter, sort: (sortKey === 'recent' && !hasCreatedAt(items) ? 'uses' : sortKey) as SortKey, search, keep: lingering })
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

  const showStarters = $derived(
    !data.degraded &&
      !items.some((c) => !c.builtin) &&
      (stateFilter === 'all' || stateFilter === 'custom') &&
      permFilter === 'all' &&
      search === ''
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
      user_cooldown: 0,
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
      user_cooldown: c.user_cooldown ?? 0,
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
    !!editorDraft && !editorDraft.builtin && committedDraft !== null && !draftsMatch(editorDraft, committedDraft, editorDraft.edit)
  );

  const discard = createDiscardGuard(
    () => isDirty,
    () => {
      if (editorDraft && !editorDraft.builtin) clearDraft(draftRef(data.board, editorDraft));
      draftVersion++;
    }
  );

  let openerKey = NEW;
  const NEW_BUTTON_ID = 'commands-new';

  function doOpenNew(seed: Partial<CommandDraft> = {}) {
    serverErrors = null;
    const draft = loadDraft({ board: data.board, name: '', edit: false }) ?? blankDraft();
    editorDraft = { ...draft, ...seed };
    expanded = NEW;
    openerKey = NEW;
    editorGen++;
  }
  function doOpenEdit(c: CommandView) {
    serverErrors = null;
    openerKey = c.name;
    if (c.builtin) {
      editorDraft = { ...blankDraft(), edit: true, name: c.name, originalName: c.name, is_active: c.is_active, builtin: true };
      expanded = c.name;
      editorGen++;
      return;
    }
    editorDraft = overlayLiveActive(loadDraft({ board: data.board, name: c.name, edit: true }) ?? fromView(c), c.is_active);
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
  function restoreFocus(key: string) {
    void tick().then(() => {
      const active = document.activeElement;
      if (active && active !== document.body) return;
      const row = document.querySelector<HTMLElement>(`[data-row-name="${CSS.escape(key)}"] .bb-row__primary`);
      (row ?? document.getElementById(NEW_BUTTON_ID))?.focus();
    });
  }

  function doCloseEditor() {
    const key = openerKey;
    expanded = null;
    editorDraft = null;
    serverErrors = null;
    draftVersion++;
    restoreFocus(key);
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
    discard.guard(() => doOpenNew({ name }));
  }

  function openStarter(starter: Starter) {
    discard.guard(() => doOpenNew({ name: normName(starter.name), response: starter.response }));
  }

  const customNames = $derived(items.filter((c) => !c.builtin).map((c) => c.name));
  function openExisting(name: string) {
    const c = items.find((x) => !x.builtin && x.name === name);
    if (c) discard.guard(() => doOpenEdit(c));
  }

  function showNewEditor(errors: CommandErrors | null, draft: CommandDraft) {
    serverErrors = errors;
    editorDraft = draft;
    expanded = NEW;
    editorGen++;
  }

  let composeDraft = $state<CommandDraft | null>(null);
  let composeBusy = $state(false);
  const matchingCustom = (name: string) => items.find((c) => !c.builtin && c.name === name);
  const composeReplaces = $derived(composeDraft !== null && !!matchingCustom(composeDraft.name));

  onMount(() => {
    discardLegacyDrafts();
    const url = new URL(window.location.href);
    if (url.searchParams.get('compose') !== '1') return;
    stageComposeLink(parseComposeLink(url.searchParams));
    for (const key of COMPOSE_PARAMS) url.searchParams.delete(key);
    setTimeout(() => replaceState(url, {}), 0);
  });

  function stageComposeLink(link: ComposeLink) {
    const draft: CommandDraft = { ...blankDraft(), ...link };
    const problems = validateCommand({
      name: draft.name,
      aliases: draft.aliases,
      response: draft.response,
      cooldown: draft.cooldown,
      userCooldown: draft.user_cooldown,
      allowedUserId: '',
      bumpCounter: ''
    });
    if (BUILTIN_NAMES.has(draft.name)) problems.name = t('commands.errBuiltinName');
    if (Object.keys(problems).length === 0) composeDraft = draft;
    else showNewEditor(problems, draft);
  }

  function composeCancel() {
    if (composeBusy) return;
    composeDraft = null;
  }

  async function composeConfirm() {
    const d = composeDraft;
    if (!d || composeBusy) return;
    composeBusy = true;
    const existing = matchingCustom(d.name);
    const view = composeView(d, existing);
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
      landCompose(view, payload);
      return;
    }
    flagError(d.name);
    showNewEditor(saveFailureErrors(payload, t), d);
    if (!serverErrors) failed(payload, t('commands.toastSaveFailed'));
  }

  function composeView(d: CommandDraft, existing: CommandView | undefined): CommandView {
    return {
      name: d.name,
      aliases: d.aliases,
      response: d.response,
      is_active: existing ? existing.is_active : true,
      stream_online_only: false,
      perm: d.perm,
      cooldown: d.cooldown,
      user_cooldown: d.user_cooldown,
      allowed_user_id: ''
    };
  }

  function landCompose(view: CommandView, payload: ActionResult) {
    applyResult(payload);
    if (!items.some((c) => c.name === view.name)) items = [...items, view];
    ackSaved(view.name);
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

  const draftedRows = $derived.by(() => {
    void draftVersion;
    return draftedNames(data.board);
  });

  const saveSubmit: SubmitFunction = ({ formData }) => {
    expectEcho(data.board);
    const d = editorDraft;
    if (!d) return;
    const key = normName(d.name);
    const orig = d.edit ? normName(d.originalName) : undefined;
    const submittedExpanded = expanded;

    const live = items.find((c) => c.name === (orig ?? key));
    const isActive = persistCommandActive(d.edit, d.is_active, live?.is_active);
    formData.set('is_active', isActive ? 'on' : '');

    const prevRows = items.filter((c) => c.name === key || c.name === orig);
    items = [...items.filter((c) => c.name !== key && c.name !== orig), optimisticView(d, key, isActive, live)];
    busy = true;
    setStatus(key, 'saving');

    return async ({ result }) => {
      busy = false;
      const payload = actionPayload<ActionResult>(result);
      const stillOpen = expanded === submittedExpanded;

      if (result.type === 'success' && payload?.ok) {
        landSave(d, key, payload, stillOpen);
        return;
      }

      items = [...items.filter((c) => c.name !== key && c.name !== orig), ...prevRows];
      flagError(orig ?? key);
      reportSaveFailure(payload, stillOpen);
    };
  };

  function optimisticView(d: CommandDraft, key: string, isActive: boolean, live: CommandView | undefined): CommandView {
    return {
      name: key,
      aliases: d.aliases.map(normName).filter(Boolean),
      response: d.response,
      is_active: isActive,
      stream_online_only: d.stream_online_only,
      perm: d.perm,
      cooldown: Math.floor(Number(d.cooldown) || 0),
      user_cooldown: Math.floor(Number(d.user_cooldown) || 0),
      allowed_user_id: d.allowed_user_id.replace(/\D/g, ''),
      bump_counter: d.bump_counter,
      uses: live?.uses,
      created_at: live?.created_at
    };
  }

  function landSave(d: CommandDraft, key: string, payload: ActionResult, stillOpen: boolean) {
    applyResult({ ...payload, silent: true });
    clearDraft(draftRef(data.board, d));
    ackSaved(key);
    if (!stillOpen) return;
    const saved = items.find((c) => c.name === key);
    if (!saved) {
      doCloseEditor();
      return;
    }
    editorDraft = fromView(saved);
    expanded = key;
    openerKey = key;
    serverErrors = null;
    editorGen++;
  }

  function reportSaveFailure(payload: ActionResult | undefined, stillOpen: boolean) {
    const errs = saveFailureErrors(payload, t);
    if (stillOpen) serverErrors = errs;
    if (errs && stillOpen) return;
    failed(payload, t(isUnavailable(payload) ? 'commands.errUnavailable' : 'commands.toastSaveFailed'));
  }

  function linger(names: string[]) {
    lingering = new Set([...lingering, ...names]);
  }

  type FailKey = 'commands.toastToggleFailed' | 'commands.toastSaveFailed';

  function settleRow(name: string, before: CommandView, payload: ActionResult | null | undefined, failKey: FailKey) {
    if (payload?.ok) {
      applyResult(payload);
      ackSaved(name);
      return;
    }
    items = items.map((x) => (x.name === name ? before : x));
    flagError(name);
    toast('danger', payload?.error ?? t(failKey));
  }

  const rowSubmit =
    (c: CommandView, patch: (formData: FormData, row: CommandView) => Partial<CommandView>, failKey: FailKey, lingers = false): SubmitFunction =>
    ({ formData }) => {
      const before = { ...c };
      items = items.map((x) => (x.name === c.name ? { ...x, ...patch(formData, x) } : x));
      if (lingers) linger([c.name]);
      setStatus(c.name, 'saving');
      return async ({ result }) => {
        settleRow(c.name, before, actionPayload<ActionResult>(result), failKey);
      };
    };

  const toggleSubmit = (c: CommandView) =>
    rowSubmit(c, (_, row) => ({ is_active: !row.is_active }), 'commands.toastToggleFailed', true);
  const replySubmit = (c: CommandView) =>
    rowSubmit(c, (formData) => ({ response: String(formData.get('reply') ?? '') }), 'commands.toastSaveFailed');
  const accessSubmit = (c: CommandView) =>
    rowSubmit(c, (formData) => ({ perm: String(formData.get('perm') ?? '') as Perm }), 'commands.toastSaveFailed');

  function postAction(action: string, body: FormData): Promise<ActionResult | null> {
    expectEcho(data.board);
    return postActionShared<ActionResult>(action, body);
  }

  function formDataFor(c: CommandView): FormData {
    const body = new FormData();
    body.set('name', c.name);
    for (const a of c.aliases ?? []) body.append('aliases', a);
    body.set('response', c.response);
    body.set('perm', c.perm ?? 'everyone');
    body.set('cooldown', String(c.cooldown ?? 0));
    body.set('user_cooldown', String(c.user_cooldown ?? 0));
    body.set('allowed_user_id', c.allowed_user_id ?? '');
    body.set('bump_counter', c.bump_counter ?? '');
    body.set('stream_online_only', c.stream_online_only ? 'on' : '');
    body.set('is_active', c.is_active ? 'on' : '');
    return body;
  }

  async function restore(snapshot: CommandView) {
    items = [...items.filter((x) => x.name !== snapshot.name), snapshot];
    setStatus(snapshot.name, 'saving');
    const body = formDataFor(snapshot);
    if (usesCount(snapshot) > 0n) body.set('restore_uses', String(usesCount(snapshot)));
    const payload = await postAction('save', body);
    if (payload?.ok) {
      applyResult({ ...payload, silent: true });
      ackSaved(snapshot.name);
      const lostUses = usesCount(snapshot) > 0n && payload.restored !== true;
      toast('success', t(lostUses ? 'commands.toastRestoredResets' : 'commands.toastRestored', { name: snapshot.name }));
    } else {
      flagError(snapshot.name);
      toast('danger', t('commands.toastCouldNotRestore', { name: snapshot.name }));
    }
  }

  const UNDO_TTL_MS = 10_000;

  async function requestDelete(c: CommandView) {
    const snapshot = { ...c, aliases: [...(c.aliases ?? [])] };
    items = items.filter((x) => x.name !== c.name);
    if (expanded === c.name) doCloseEditor();
    clearDraft({ board: data.board, name: c.name, edit: true });
    draftVersion++;

    let undone = false;
    toast('success', t('commands.toastDeletedShort', { name: c.name }), {
      ttlMs: UNDO_TTL_MS,
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
      toast('danger', payload?.error ?? t('commands.toastDeleteFailed', { name: c.name }));
    }
  }

  let selecting = $state(false);
  let selected = $state<ReadonlySet<string>>(new Set());
  let bulkBusy = $state(false);
  let bulkDeleteOpen = $state(false);

  const selectedCustom = $derived([...selected].filter((n) => items.some((c) => c.name === n && !c.builtin)));

  function toggleSelect(name: string) {
    const next = new Set(selected);
    if (!next.delete(name)) next.add(name);
    selected = next;
  }
  function selectAllShown() {
    selected = new Set(rows.map((c) => c.name));
  }
  function exitSelect() {
    selecting = false;
    selected = new Set();
  }
  function enterSelect() {
    discard.guard(() => {
      if (editorDraft) doCloseEditor();
      selecting = true;
    });
  }
  const toggleSelecting = () => (selecting ? exitSelect() : enterSelect());
  const onRowActivate = (c: CommandView) => (selecting ? toggleSelect(c.name) : openEdit(c));

  type BulkOp = 'enable' | 'disable' | 'delete';
  const BULK_DONE_KEYS = {
    enable: 'commands.bulkEnabled',
    disable: 'commands.bulkDisabled',
    delete: 'commands.bulkDeleted'
  } as const;

  function applyBulk(op: BulkOp, names: string[]) {
    const set = new Set(names);
    items =
      op === 'delete'
        ? items.filter((c) => !set.has(c.name))
        : items.map((c) => (set.has(c.name) ? { ...c, is_active: op === 'enable' } : c));
    if (op !== 'delete') linger(names);
    for (const n of names) setStatus(n, 'saving');
  }

  function revertBulk(before: CommandView[], failedNames: Set<string>) {
    const back = before.filter((c) => failedNames.has(c.name));
    items = [...items.filter((c) => !failedNames.has(c.name)), ...back];
  }

  function settleBulk(op: BulkOp, before: CommandView[], payload: ActionResult | null) {
    const results = payload?.ok && payload.results ? payload.results : before.map((c) => ({ name: c.name, ok: false }));
    const failedNames = new Set(results.filter((r) => !r.ok).map((r) => r.name));
    const doneCount = results.length - failedNames.size;
    revertBulk(before, failedNames);
    for (const r of results) (r.ok ? ackSaved : flagError)(r.name);
    selected = failedNames;
    if (!payload?.ok) toast('danger', t('commands.bulkFailed'));
    else if (doneCount > 0) toast('success', t(BULK_DONE_KEYS[op], { count: numberFormat.format(doneCount) }));
    if (payload?.ok && failedNames.size > 0) toast('danger', t('commands.bulkPartial', { failed: numberFormat.format(failedNames.size) }));
  }

  async function runBulk(op: BulkOp) {
    const names = op === 'delete' ? selectedCustom : [...selected];
    if (names.length === 0 || bulkBusy) return;
    bulkBusy = true;
    const before = items.filter((c) => names.includes(c.name));
    if (expanded && names.includes(expanded)) doCloseEditor();
    applyBulk(op, names);
    const body = new FormData();
    body.set('op', op);
    for (const n of names) body.append('name', n);
    const payload = await postAction('bulk', body);
    settleBulk(op, before, payload);
    bulkBusy = false;
  }

  function confirmBulkDelete() {
    bulkDeleteOpen = false;
    void runBulk('delete');
  }

  const rovingHandlers = {
    onSpace(name: string) {
      if (selecting) return toggleSelect(name);
      if (rowStatus[name] === 'saving') return;
      document.querySelector<HTMLFormElement>(`[data-row-name="${CSS.escape(name)}"] form`)?.requestSubmit();
    },
    onDelete(name: string) {
      const c = items.find((x) => x.name === name);
      if (c && !c.builtin && !selecting) void requestDelete(c);
    }
  };

  const activeCount = $derived(items.filter((c) => c.is_active).length);

  const selectedCmd = $derived(
    expanded && expanded !== NEW ? items.find((c) => c.name === expanded) : undefined
  );

  function setSelectedActive(next: boolean) {
    const c = selectedCmd;
    if (!c || c.builtin || c.is_active === next) return;
    const before = { ...c };
    items = items.map((x) => (x.name === c.name ? { ...x, is_active: next } : x));
    linger([c.name]);
    setStatus(c.name, 'saving');
    void postAction('toggle', formDataFor({ ...c, is_active: next })).then((payload) => {
      settleRow(c.name, before, payload, 'commands.toastToggleFailed');
    });
  }

  type FooterStatus = 'idle' | 'saving' | 'saved' | 'error' | 'conflict';
  function footerStatus(): FooterStatus {
    if (busy) return 'saving';
    const s = rowStatus[expanded ?? ''] ?? 'idle';
    return s === 'live' ? 'saved' : (s as FooterStatus);
  }

  let searchInput = $state<HTMLInputElement | undefined>(undefined);

  function onSearchKey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      if (search) search = '';
      else searchInput?.blur();
    } else if (e.key === 'Enter' && canCreateTyped && rows.length === 0) {
      e.preventDefault();
      createTyped();
    }
  }

  function onKey(e: KeyboardEvent) {
    if (composeDraft !== null || discard.open || bulkDeleteOpen) return;
    if (!isShortcut(e)) return;
    if (e.key === 'Escape' && selecting && !editorDraft) {
      exitSelect();
    } else if (e.key === '/') {
      e.preventDefault();
      searchInput?.focus();
    } else if (e.key === 'n' || e.key === 'N') {
      e.preventDefault();
      openNew();
    }
  }
</script>

<section class="screen active" use:warmOnIntent={[loadCommandEditor, loadBuiltinInspector, loadChatPreview]}>
  <PageHead eyebrow={t('commands.eyebrow')} description={t('commands.description')}>
    {t('commands.titlePre')}<em>{t('commands.titleEm')}</em>
    {#snippet trailing()}
      <div class="deck-stat-block">
      <Divider flush />
      <dl class="deck-stats">
        <div class="ds-cell">
          <dt><Label mono as="span">{t('commands.statActive')}</Label></dt>
          <dd><span class="big">{activeCount}</span><Text as="span" size="xs" mono tone="muted">/{items.length}</Text></dd>
        </div>
        <Divider vertical flush />
        <div class="ds-cell">
          <dt><Label mono as="span">{t('commands.statFires')}</Label></dt>
          <dd><span class="big">{formatCounterValue(fires.toString())}</span></dd>
        </div>
        <Divider vertical flush />
        <div class="ds-cell">
          <dt><Label mono as="span">{t('commands.statBusiest')}</Label></dt>
          <dd><Text as="span" mono tone="accent">{busiest ? `!${busiest.name}` : t('commands.statNone')}</Text></dd>
        </div>
      </dl>
      <Divider flush />
      </div>
      {#if data.publicPage}
        <div class="public-page"><PublicPageChip on={data.publicPage.on} url={data.publicPage.url} /></div>
      {/if}
    {/snippet}
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('commands.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet leading()}
      <div class="tb-lead">
        <SegmentedControl options={stateOptions} bind:value={stateValue.get, stateValue.set} label={t('commands.filterLabel')} />
        <div class="tb-select">
          <Select fill bind:value={permFilter} options={permOptions} label={t('commands.permLabel')} />
        </div>
        <div class="tb-select">
          <Select fill bind:value={sortKey} options={sortOptions} label={t('commands.sortLabel')} />
        </div>
      </div>
    {/snippet}
    {#snippet trailing()}
      <span class="keys" aria-hidden="true"><Kbd>/</Kbd> {t('commands.keysSearch')} <Kbd>N</Kbd> {t('commands.keysNew')} <Kbd>Esc</Kbd> {t('commands.keysClear')}</span>
      <div class="toolbar-search">
        <SearchInput placeholder={t('commands.searchPlaceholder')} clearLabel={t('quotes.searchClear')}
          aria-label={t('commands.searchPlaceholder')} bind:value={search} bind:element={searchInput} onkeydown={onSearchKey} fill />
      </div>
      <Button variant="secondary" aria-pressed={selecting} onclick={toggleSelecting}>
        {selecting ? t('commands.selectDone') : t('commands.selectMode')}
      </Button>
      <Button variant="primary" id={NEW_BUTTON_ID} onclick={openNew} disabled={expanded === NEW}>
        {t('commands.newCommand')}
      </Button>
    {/snippet}
  </PageToolbar>

  <DeckLayout inspecting={!!editorDraft}>
    <div class="main-col">
      <DeckList>
        <div class="list" use:rovingList={rovingHandlers}>
          {#if showStarters}
            <div class="group">
              <div class="group-head">
                <Eyebrow>{t('commands.groupYours')}</Eyebrow>
                <span class="g-rule" aria-hidden="true"></span>
                <Label mono as="span">{t('commands.groupYoursNote')}</Label>
              </div>
              <StarterCommands onNew={openNew} onPick={openStarter} />
            </div>
          {/if}
          {#each groups as g (g.key)}
            <div class="group">
              <div class="group-head">
                <Eyebrow>{g.label}</Eyebrow>
                <Text as="span" size="xs" mono tone="muted">{g.rows.length}</Text>
                <span class="g-rule" aria-hidden="true"></span>
                <Label mono as="span">{g.note}</Label>
              </div>
              {#each g.rows as c, i (c.name)}
                <CommandRow
                  command={c}
                  index={i + 1}
                  {usesMax}
                  status={rowStatus[c.name] ?? 'idle'}
                  unsaved={draftedRows.has(c.name) && expanded !== c.name}
                  expanded={!selecting && expanded === c.name}
                  {selecting}
                  checked={selected.has(c.name)}
                  onExpand={() => onRowActivate(c)}
                  onDelete={() => requestDelete(c)}
                  toggleSubmit={toggleSubmit(c)}
                />
              {/each}
            </div>
          {/each}
          {#if rows.length === 0 && !showStarters}
            <EmptyState title={t('commands.noneMatch')} body={t('commands.noneMatchSub')} />
          {/if}
          <div class="create-hint" class:on={canCreateTyped}>
            <Button variant="add" onclick={createTyped} aria-label={t('commands.createHintAria', { name: typedName })}>
              <span class="ch-name">!{typedName}</span>
              <Text as="span" size="sm" tone="muted">{t('commands.createHint')}</Text>
              {t('commands.createHintCta')}
            </Button>
          </div>
        </div>
      </DeckList>
      {#if selecting}
        <BulkBar
          count={selected.size}
          deletable={selectedCustom.length}
          busy={bulkBusy}
          onEnable={() => runBulk('enable')}
          onDisable={() => runBulk('disable')}
          onDelete={() => (bulkDeleteOpen = true)}
          onAll={selectAllShown}
          onDone={exitSelect}
        />
      {/if}
    </div>

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
              {#await loadBuiltinInspector() then { default: BuiltinInspector }}
                <BuiltinInspector
                  command={selectedCmd}
                  {def}
                  toggleSubmit={toggleSubmit(selectedCmd)}
                  replySubmit={replySubmit(selectedCmd)}
                  accessSubmit={accessSubmit(selectedCmd)}
                  {busy}
                />
              {/await}
            </Scroller>
          {/if}
        {:else}
          {#key expanded + '#' + editorGen}
            {#await loadCommandEditor() then { default: CommandEditor }}
              <CommandEditor
                bind:draft={editorDraft}
                board={data.board}
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
                {customNames}
                onOpenExisting={openExisting}
              />
            {/await}
          {/key}
        {/if}
      </InspectorSurface>
    {/if}
  </DeckLayout>
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
      <Text as="span" size="xs" mono tone="muted">{tPerm(t, composeDraft.perm)}</Text>
      <Text as="span" size="xs" mono tone="muted">{t('commandRow.cooldown')} {composeDraft.cooldown}s</Text>
      {#if composeDraft.aliases.length}
        <Text as="span" size="xs" mono tone="muted">{t('commandRow.also', { aliases: composeDraft.aliases.map((a) => '!' + a).join(' ') })}</Text>
      {/if}
    </div>
    {#await loadChatPreview() then { default: ChatPreview }}
      <ChatPreview name={composeDraft.name} response={composeDraft.response} />
    {/await}
  {/if}
</ConfirmDialog>

<ConfirmDialog
  open={bulkDeleteOpen}
  title={t('commands.bulkDeleteTitle')}
  body={t('commands.bulkDeleteBody', { count: numberFormat.format(selectedCustom.length) })}
  confirmLabel={t('commands.bulkDeleteConfirm', { count: numberFormat.format(selectedCustom.length) })}
  cancelLabel={t('common.cancel')}
  onCancel={() => (bulkDeleteOpen = false)}
  onConfirm={confirmBulkDelete}
  tone="danger"
/>

<ConfirmDialog
  open={discard.open}
  title={t('commands.discardTitle')}
  body={t('commands.discardBody')}
  confirmLabel={t('commands.discard')}
  cancelLabel={t('commands.keepEditing')}
  onCancel={discard.cancel}
  onConfirm={discard.confirm}
  tone="danger"
/>

<svelte:window onkeydown={onKey} />
<svelte:document onvisibilitychange={() => {
  if (data.delegateOf && document.visibilityState === 'visible') void invalidate('app:commands');
}} />

<style>
  .compose-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 14px;
    margin-bottom: 4px;
  }

  .deck-stats {
    display: flex;
    gap: 26px;
    margin: 0;
    padding: 14px 0;
  }
  .ds-cell { white-space: nowrap; }
  .deck-stats dt { margin-bottom: 6px; }
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

  .list { container-type: inline-size; }
  .main-col { min-width: 0; }

  .tb-lead { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; }
  .tb-select { width: 168px; }
  .public-page { margin-top: 10px; }

  .create-hint {
    --btn-w: 100%;
    --btn-min-h: 52px;
    margin: 14px;
    visibility: hidden;
    opacity: 0;
  }
  .create-hint.on { visibility: visible; opacity: 1; }
  .ch-name { font-family: var(--bb-font-mono); }

  .group + .group { margin-top: 26px; }
  .group-head {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 0 14px 9px;
  }
  .g-rule { flex: 1; height: 1px; background: var(--bb-border); }

  .toolbar-search { width: 220px; flex-shrink: 0; }

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
  @media (hover: hover) and (pointer: fine) and (min-width: 1440px) {
    .keys { display: inline-flex; }
  }

  @media (max-width: 760px) {
    .deck-stats { gap: 16px; }
    .deck-stats .big { font-size: 20px; }
    .toolbar-search { width: 100%; order: 3; }
    .tb-select { flex: 1; min-width: 140px; }
  }
</style>
