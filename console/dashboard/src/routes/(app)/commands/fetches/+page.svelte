<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // /commands/fetches — urlfetch definition builder & rehearsal. Deck-list +
  // docked-inspector like the commands page; keys custody card below; every
  // write rides a progressive-enhancement <form> against this route's own
  // actions (save/delete/setkey/delkey/testrun).
  import { deserialize, enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    AlertBanner,
    ButtonLink,
    Card,
    CardHead,
    ConfirmDialog,
    DeckList,
    EmptyState,
    Icon,
    InspectorSurface,
    PageHead,
    PageToolbar,
    toast,
    DEFS_PER_BROADCASTER,
    COMMAND_SAMPLES,
    urlFetchNames,
    getI18n,
    type CommandView,
    type FetchDefErrors,
    type Samples
  } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';
  import type { FetchDefView, FetchKeyView } from '$lib/server/fetches-store';
  import FetchDefEditor from '$lib/components/commands/fetches/FetchDefEditor.svelte';
  import FetchKeyManager from '$lib/components/commands/fetches/FetchKeyManager.svelte';
  import {
    clearFetchDraft,
    loadFetchDraft,
    type FetchDraft
  } from '$lib/components/commands/fetches/drafts';

  let { data } = $props();

  const { t } = getI18n();

  // Local source of truth seeded from SSR; each action result reconciles its
  // own rows (order-independent under concurrent submits).
  // svelte-ignore state_referenced_locally
  let defs = $state<FetchDefView[]>(data.defs ?? []);
  // svelte-ignore state_referenced_locally
  let keys = $state<FetchKeyView[]>(data.keys ?? []);

  // Command responses, for the client-side reference pre-warnings. Never
  // edited here.
  // svelte-ignore state_referenced_locally
  let commands = $state<CommandView[]>(data.commands ?? []);

  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      defs = data.defs ?? [];
      keys = data.keys ?? [];
      commands = data.commands ?? [];
    }
  });

  type ActionResult = {
    ok: boolean;
    action?: 'created' | 'updated' | 'deleted';
    name?: string;
    original?: string;
    silent?: boolean;
    error?: string;
    errors?: FetchDefErrors;
    defs?: FetchDefView[];
    keys?: FetchKeyView[];
    last4?: string;
    // testrun replies
    status?: string;
    values?: string[];
    ms?: number;
  };

  function upsertRows(rows: FetchDefView[]) {
    if (!rows.length) return;
    for (const row of rows) {
      defs = [...defs.filter((d) => d.name !== row.name), row];
    }
  }

  // Reconcile one result; the affected row(s) come from the payload's echoed
  // lists, never a wholesale snapshot replacement.
  function applyDefsResult(d: ActionResult) {
    if (!d.ok) {
      if (d.error) toast('err', d.error);
      return;
    }
    if (d.action === 'deleted') {
      defs = defs.filter((x) => x.name !== d.name);
      if (d.keys) keys = d.keys;
      if (!d.silent) toast('ok', t('fetches.toastDeleted', { name: d.name ?? '' }));
      return;
    }
    upsertRows(d.defs ?? []);
    if (d.keys) keys = d.keys;
    if (!d.silent) toast('ok', t(d.action === 'created' ? 'fetches.toastCreated' : 'fetches.toastUpdated', { name: d.name ?? '' }));
  }

  // --- Per-name save-state machine (commands-page shape) ---------------------
  let rowStatus = $state<Record<string, SaveState>>({});
  const statusTimers = new Map<string, ReturnType<typeof setTimeout>[]>();

  function setStatus(name: string, s: SaveState) {
    for (const timer of statusTimers.get(name) ?? []) clearTimeout(timer);
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

  // --- Editor -----------------------------------------------------------------
  const NEW = '__new__';
  let expanded = $state<string | null>(null);
  let editorDraft = $state<FetchDraft | null>(null);
  let serverErrors = $state<FetchDefErrors | null>(null);
  let busy = $state(false);

  function blankDraft(): FetchDraft {
    return {
      edit: false,
      displayName: '',
      name: '',
      originalName: '',
      url: '',
      kind: 'plain',
      path: [],
      key_label: '',
      is_active: true,
      template: ''
    };
  }

  function fromDef(d: FetchDefView): FetchDraft {
    return {
      edit: true,
      displayName: d.name,
      name: d.name,
      originalName: d.name,
      url: d.url,
      kind: d.json_path.length > 0 ? 'json' : 'plain',
      path: [...d.json_path],
      key_label: d.key_label ?? '',
      is_active: d.is_active,
      template: ''
    };
  }

  let editorGen = $state(0);

  const committedDraft = $derived.by<FetchDraft | null>(() => {
    if (!editorDraft || !editorDraft.edit) return blankDraft();
    const def = defs.find((d) => d.name === editorDraft!.originalName);
    return def ? fromDef(def) : null;
  });
  const isDirty = $derived(
    !!editorDraft && committedDraft !== null ? JSON.stringify(editorDraft) !== JSON.stringify(committedDraft) : false
  );

  // Dirty guard routing through one confirmation (commands-page shape).
  let discardOpen = $state(false);
  let afterDiscard: (() => void) | null = null;
  function guarded(action: () => void) {
    if (isDirty && editorDraft) {
      afterDiscard = action;
      discardOpen = true;
    } else {
      action();
    }
  }
  function confirmDiscard() {
    discardOpen = false;
    if (editorDraft) clearFetchDraft(editorDraft.originalName, editorDraft.edit);
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
    resetTest();
    editorDraft = loadFetchDraft('', false) ?? blankDraft();
    expanded = NEW;
    editorGen++;
  }
  function doOpenEdit(d: FetchDefView) {
    serverErrors = null;
    resetTest();
    editorDraft = loadFetchDraft(d.name, true) ?? fromDef(d);
    expanded = d.name;
    editorGen++;
  }
  function doCloseEditor() {
    expanded = null;
    editorDraft = null;
    serverErrors = null;
  }
  function openNew() {
    guarded(doOpenNew);
  }
  function openEdit(d: FetchDefView) {
    if (expanded === d.name) {
      closeEditor();
      return;
    }
    guarded(() => doOpenEdit(d));
  }
  function closeEditor() {
    guarded(doCloseEditor);
  }

  type FooterStatus = 'idle' | 'saving' | 'saved' | 'error' | 'conflict';
  function footerStatus(): FooterStatus {
    if (busy) return 'saving';
    const s = rowStatus[expanded ?? ''] ?? 'idle';
    return s as FooterStatus;
  }

  // --- Save (optimistic with row-level rollback) ------------------------------
  function defFormBody(d: FetchDraft, isActive: boolean): FormData {
    const body = new FormData();
    body.set('name', d.name);
    body.set('url', d.url);
    body.set('kind', d.kind);
    body.set('path', d.path.join('.'));
    body.set('key_label', d.key_label);
    body.set('is_active', isActive ? 'on' : '');
    return body;
  }

  const saveSubmit: SubmitFunction = () => {
    const d = editorDraft;
    if (!d) return;
    const orig = d.edit ? d.originalName : undefined;

    const prevRows = defs.filter((x) => x.name === d.name || x.name === orig);
    const optimistic: FetchDefView = {
      name: d.name,
      url: d.url,
      json_path: d.kind === 'json' ? [...d.path] : [],
      is_active: d.is_active,
      key_label: d.key_label
    };
    defs = [...defs.filter((x) => x.name !== d.name && x.name !== orig), optimistic];
    busy = true;
    setStatus(d.name, 'saving');
    const submittedExpanded = expanded;

    return async ({ result }) => {
      busy = false;
      const payload =
        result.type === 'success' || result.type === 'failure'
          ? (result.data as ActionResult | undefined)
          : undefined;
      const stillOpen = expanded === submittedExpanded;

      if (result.type === 'success' && payload?.ok) {
        applyDefsResult({ ...payload, silent: true });
        clearFetchDraft(orig ?? '', !!orig);
        ackSaved(d.name);
        if (stillOpen) {
          const saved = defs.find((x) => x.name === d.name);
          if (saved) {
            editorDraft = fromDef(saved);
            expanded = saved.name;
            serverErrors = null;
            editorGen++;
          } else {
            doCloseEditor();
          }
        }
        return;
      }

      // Rollback the affected rows; keep the editor open with the draft intact.
      defs = [...defs.filter((x) => x.name !== d.name && x.name !== orig), ...prevRows];
      flagError(orig ?? d.name);
      if (stillOpen) serverErrors = payload?.errors ?? null;
      if (!payload?.errors) toast('err', payload?.error ?? t('fetches.toastSaveFailed'));
    };
  };

  // Lightweight active toggle straight from the row (its own mini-form posts
  // the whole definition with just the flag flipped — one verb, one write).
  const toggleSubmit =
    (d: FetchDefView): SubmitFunction =>
    () => {
      const before = { ...d };
      const flipped = !d.is_active;
      const optimisticRow = { ...before, is_active: flipped };
      defs = defs.map((x) => (x.name === d.name ? optimisticRow : x));
      setStatus(d.name, 'saving');

      return async ({ result }) => {
        const payload =
          result.type === 'success' || result.type === 'failure'
            ? (result.data as ActionResult | undefined)
            : undefined;
        if (result.type === 'success' && payload?.ok) {
          // Reconcile from the echo; fall back to the optimistic row when the
          // read-back came back empty (write landed, refresh failed).
          if (payload.defs?.length) {
            upsertRows(payload.defs);
            if (payload.keys) keys = payload.keys;
          }
          ackSaved(d.name);
        } else {
          defs = defs.map((x) => (x.name === d.name ? before : x));
          flagError(d.name);
          toast('err', payload?.error ?? t('fetches.toastToggleFailed'));
        }
      };
    };

  // --- postAction (deserialize pattern, commands/+page.svelte:542) ------------
  async function postAction(action: string, body: FormData): Promise<ActionResult | null> {
    try {
      const res = await fetch(`?/${action}`, { method: 'POST', body });
      const result = deserialize(await res.text());
      return result.type === 'success' || result.type === 'failure'
        ? ((result.data as ActionResult | undefined) ?? null)
        : null;
    } catch {
      return null;
    }
  }

  // --- Delete definition (pre-warn, then optimistic remove) --------------------
  let deleteTarget = $state<FetchDefView | null>(null);
  let deleteBusy = $state(false);

  // Commands whose responses embed `{urlfetch:<defName>}` — a client-side
  // pre-warning only; the service refuses still-referenced deletes itself.
  function referencingCommands(defName: string): string[] {
    const needle = `{urlfetch:${defName.toLowerCase()}}`;
    return commands
      .filter((c) => c.builtin !== true && c.response.toLowerCase().includes(needle))
      .map((c) => c.name);
  }
  const deleteRefs = $derived(deleteTarget ? referencingCommands(deleteTarget.name) : []);

  async function confirmDeleteDef() {
    const target = deleteTarget;
    if (!target || deleteBusy) return;
    deleteBusy = true;
    const prevRows = [...defs];
    defs = defs.filter((x) => x.name !== target.name);
    if (expanded === target.name) doCloseEditor();
    clearFetchDraft(target.name, true);

    const body = new FormData();
    body.set('name', target.name);
    const payload = await postAction('delete', body);
    deleteBusy = false;
    deleteTarget = null;
    if (payload?.ok) {
      applyDefsResult({ ...payload, silent: true });
      toast('ok', t('fetches.toastDeleted', { name: target.name }));
    } else {
      defs = prevRows;
      flagError(target.name);
      toast('err', payload?.error ?? t('fetches.toastDeleteFailed', { name: target.name }));
    }
  }

  // --- Keys -------------------------------------------------------------------
  let keyBusy = $state(false);

  async function handleSetKey(label: string, value: string) {
    if (keyBusy) return;
    keyBusy = true;
    const body = new FormData();
    body.set('label', label);
    body.set('value', value);
    const payload = await postAction('setkey', body);
    keyBusy = false;
    if (payload?.ok) {
      if (payload.keys) keys = payload.keys;
      toast('ok', t('fetches.keySavedToast', { label, last4: payload.last4 ?? '····' }));
    } else {
      toast('err', payload?.error ?? t('fetches.keySaveFailed'));
    }
  }

  async function handleDeleteKey(label: string) {
    if (keyBusy) return;
    keyBusy = true;
    const body = new FormData();
    body.set('label', label);
    const payload = await postAction('delkey', body);
    keyBusy = false;
    // Only the keys list changes on a key delete (defs keep dangling labels
    // and fail closed), so defs are left untouched here.
    if (payload?.ok) {
      if (payload.keys) keys = payload.keys;
      toast('ok', t('fetches.keyDeletedToast', { label }));
    } else {
      toast('err', payload?.error ?? t('fetches.keyDeleteFailed'));
    }
  }

  // label -> command names affected if that key disappears: any command whose
  // response embeds a def bound to the label fails closed without it.
  const keyReferences = $derived.by<Record<string, string[]>>(() => {
    const map: Record<string, string[]> = {};
    for (const def of defs) {
      const label = def.key_label;
      if (!label) continue;
      const refs = referencingCommands(def.name);
      map[label] = [...new Set([...(map[label] ?? []), ...refs])];
    }
    return map;
  });

  // --- Rehearsal dry-run --------------------------------------------------------
  let testing = $state(false);
  let testError = $state('');
  let testStatus = $state('');
  let testMs = $state<number | null>(null);
  let testValues = $state<Samples>({});
  let testRunId = $state(0);

  function resetTest() {
    testError = '';
    testStatus = '';
    testMs = null;
    testValues = {};
    testRunId++;
  }

  // Samples merged over COMMAND_SAMPLES: ChatPreview resolves {urlfetch:key}
  // spans through the same lookup the engine's tokens take, so unresolved
  // slots stay literal and red-marked exactly like chat's unknown tokens.
  const testSamples = $derived.by<Samples | undefined>(() => {
    void testRunId;
    if (Object.keys(testValues).length === 0) return undefined;
    return { ...COMMAND_SAMPLES, ...testValues };
  });

  async function runTest() {
    const d = editorDraft;
    if (!d || testing) return;
    testing = true;
    testError = '';
    testStatus = '';
    testMs = null;
    testValues = {};

    const payload = await postAction('testrun', defFormBody(d, d.is_active));
    testing = false;

    if (!payload) {
      testError = t('fetches.testNoAnswer');
      return;
    }
    if (!payload.ok) {
      testError = payload.error ?? t('fetches.testNoAnswer');
      return;
    }

    const status = payload.status ?? 'upstream_error';
    testStatus = status;
    testMs = payload.ms ?? null;

    // Values arrive positionally (gossip caps replies server-side); they zip
    // onto the template's distinct tokens in sesame's first-appearance scan
    // order. Failure statuses merge the exact static texts chat renders
    // (Phase 4 failure table); bad_def merges nothing — tokens stay verbatim,
    // which IS the authoring signal.
    const names = urlFetchNames(d.template);
    const next: Record<string, string> = {};
    if (status === 'ok') {
      names.forEach((n, i) => {
        if (i < (payload.values ?? []).length) next[`urlfetch:${n}`] = payload.values![i];
      });
    } else if (status === 'denied' || status === 'limited') {
      for (const n of names) next[`urlfetch:${n}`] = t('fetches.fallbackUnavailable');
    } else if (status === 'upstream_error') {
      for (const n of names) next[`urlfetch:${n}`] = t('fetches.fallbackSourceError');
    } else if (status === 'timeout') {
      for (const n of names) next[`urlfetch:${n}`] = t('fetches.fallbackTimeout');
    }
    testValues = next;
    testRunId++;
  }

  const quotaFull = $derived(defs.length >= DEFS_PER_BROADCASTER);
</script>

<section class="screen active">
  <PageHead eyebrow={t('fetches.eyebrow')} description={t('fetches.description', { count: String(defs.length) })}>
    {t('fetches.titlePre')}<em>{t('fetches.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('fetches.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet trail()}
      <span class="quota" class:full={quotaFull}>{t('fetches.quotaHint', { count: String(defs.length), max: String(DEFS_PER_BROADCASTER) })}</span>
      <ButtonLink href="/commands" variant="ghost"><Icon name="commands" size={14} /> {t('fetches.backToCommands')}</ButtonLink>
      <button class="btn primary" onclick={openNew} disabled={quotaFull || expanded === NEW}>
        <Icon name="plus" size={14} /> {t('fetches.newDef')}
      </button>
    {/snippet}
  </PageToolbar>

  <div class="deck {editorDraft ? 'inspecting' : ''}">
    <DeckList>
      <div class="stack">
        <div class="list">
          {#each defs.toSorted((a, b) => a.name.localeCompare(b.name)) as d (d.name)}
            <div class="row" class:open={expanded === d.name}>
              <button type="button" class="row-main" onclick={() => openEdit(d)}>
                <span class="row-name">{`{urlfetch:${d.name}}`}</span>
                <span class="row-kind">{d.json_path.length > 0 ? `json · ${d.json_path.join('.')}` : 'plain'}</span>
                <span class="row-url">{d.url}</span>
                <span class="row-badges">
                  {#if d.key_label}<span class="badge key" title={t('fetches.badgeKey')}><Icon name="lock" size={11} /> {d.key_label}</span>{/if}
                  <span class="badge {d.is_active ? 'on' : 'off'}">{d.is_active ? t('fetches.active') : t('fetches.paused')}</span>
                  {#if rowStatus[d.name] === 'saving'}<span class="badge saving">{t('commandEditor.saving')}</span>
                  {:else if rowStatus[d.name] === 'saved'}<span class="badge saved">{t('commands.saved')}</span>
                  {:else if rowStatus[d.name] === 'error'}<span class="badge off">{t('fetches.saveFailedShort')}</span>
                  {/if}
                </span>
              </button>
              <div class="row-acts">
                <!-- Progressive-enhancement toggle: native POST works without JS. -->
                <form method="POST" action="?/save" use:enhance={toggleSubmit(d)}>
                  <input type="hidden" name="name" value={d.name} />
                  <input type="hidden" name="original_name" value={d.name} />
                  <input type="hidden" name="edit" value="1" />
                  <input type="hidden" name="url" value={d.url} />
                  <input type="hidden" name="kind" value={d.json_path.length > 0 ? 'json' : 'plain'} />
                  <input type="hidden" name="path" value={d.json_path.join('.')} />
                  <input type="hidden" name="key_label" value={d.key_label} />
                  <input type="hidden" name="is_active" value={d.is_active ? '' : 'on'} />
                  <button type="submit" class="mini" disabled={busy}>{d.is_active ? t('fetches.pause') : t('fetches.resume')}</button>
                </form>
                <button type="button" class="mini danger" onclick={() => (deleteTarget = d)} aria-label={t('fetches.deleteAria', { name: d.name })}>
                  <Icon name="trash" size={12} />
                </button>
              </div>
            </div>
          {/each}
          {#if defs.length === 0}
            <EmptyState icon="link" title={t('fetches.noneYet')} body={t('fetches.noneYetSub')}>
              <button class="btn primary" onclick={openNew}><Icon name="plus" size={14} /> {t('fetches.newDef')}</button>
            </EmptyState>
          {/if}
        </div>

        <Card>
          <CardHead title={t('fetches.keysTitle')} />
          <FetchKeyManager {keys} references={keyReferences} busy={keyBusy} onSetKey={handleSetKey} onDeleteKey={handleDeleteKey} />
        </Card>
      </div>

      {#if editorDraft}
        <InspectorSurface
          open
          title={editorDraft.edit ? t('fetches.editing', { name: editorDraft.originalName }) : t('fetches.newDef')}
          controls="fetch-editor"
          closeLabel={t('commands.closeEditor')}
          onClose={closeEditor}
        >
          {#key expanded + '#' + editorGen}
            <FetchDefEditor
              bind:draft={editorDraft}
              defs={defs.map(({ name }) => ({ name }))}
              keys={keys.map(({ label }) => ({ label }))}
              status={footerStatus()}
              dirty={isDirty}
              testing={testing}
              testError={testError}
              testStatus={testStatus}
              testSamples={testSamples}
              onCancel={closeEditor}
              onSubmit={saveSubmit}
              onTest={runTest}
            />
          {/key}
        </InspectorSurface>
      {/if}
    </DeckList>
  </div>
</section>

<!-- Definition delete: pre-warns with referencing commands; the service owns
     the authoritative refusal. No undo — deletes are immediate server-side. -->
<ConfirmDialog
  open={deleteTarget !== null}
  title={t('fetches.deleteTitle', { name: deleteTarget?.name ?? '' })}
  confirmLabel={t('common.delete')}
  cancelLabel={t('common.cancel')}
  danger
  busy={deleteBusy}
  onConfirm={confirmDeleteDef}
  onCancel={() => (deleteTarget = null)}
>
  {#if deleteRefs.length > 0}
    <p class="ref-warn">{t('fetches.deleteRefs')}</p>
    <ul class="ref-list">
      {#each deleteRefs as name (name)}
        <li><code>{`{urlfetch:${name}}`}</code></li>
      {/each}
    </ul>
  {:else}
    <p class="ref-note">{t('fetches.deleteSafe')}</p>
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

<style>
  .deck {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  @media (min-width: 1080px) {
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 420px; }
  }

  .stack { display: flex; flex-direction: column; gap: 16px; min-width: 0; }
  .list { display: flex; flex-direction: column; }

  .quota {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-muted);
    white-space: nowrap;
  }
  .quota.full { color: #cf8a78; }

  .row {
    display: flex;
    align-items: stretch;
    gap: 8px;
    padding: 12px 14px;
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    transition: background var(--bb-dur-fast, 140ms) ease;
  }
  .list :global(.row:last-child), .row:last-child { border-bottom: none; }
  .row:hover { background: rgba(255, 255, 255, 0.02); }
  .row.open { background: rgba(201, 168, 124, 0.05); }

  .row-main {
    flex: 1;
    min-width: 0;
    display: grid;
    grid-template-columns: auto auto 1fr auto;
    align-items: baseline;
    gap: 6px 14px;
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    text-align: left;
  }
  .row-name {
    font-family: var(--bb-font-mono);
    font-size: 13px;
    color: var(--bb-green-glow, #52b788);
    white-space: nowrap;
  }
  .row-kind {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 220px;
  }
  .row-url {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-tan-light);
    opacity: 0.75;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .row-badges { display: inline-flex; gap: 6px; align-items: center; justify-self: end; }

  .badge {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 10.5px;
    letter-spacing: 0.03em;
    border-radius: 999px;
    padding: 2px 9px;
    white-space: nowrap;
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .badge.on { color: var(--bb-green-glow, #52b788); background: rgba(82, 183, 136, 0.1); }
  .badge.off { color: var(--bb-muted); background: rgba(255, 255, 255, 0.04); }
  .badge.key { color: var(--bb-tan-light); background: rgba(201, 168, 124, 0.1); }
  .badge.saved { color: var(--bb-tan-light); }
  .badge.saving { color: var(--bb-muted); }

  .row-acts { display: flex; align-items: center; gap: 8px; flex: none; }

  .mini {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 11px;
    color: var(--bb-tan);
    background: transparent;
    border: 1px solid var(--glass-border);
    border-radius: 999px;
    padding: 5px 12px;
    cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) ease;
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .mini:hover:not(:disabled) { color: var(--bb-tan-pale); border-color: var(--bb-border-strong); }
  .mini:disabled { opacity: 0.45; cursor: default; }
  .mini.danger:hover:not(:disabled) { color: #cf8a78; border-color: rgba(176, 90, 70, 0.5); }

  .ref-warn { margin: 0 0 8px; font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-white); }
  .ref-list { list-style: none; margin: 0 0 8px; padding: 0; display: flex; flex-wrap: wrap; gap: 6px; }
  .ref-list code {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: #cf8a78;
    background: rgba(176, 90, 70, 0.12);
    border-radius: 999px;
    padding: 2px 9px;
  }
  .ref-note { margin: 0; font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-muted); }

  @media (max-width: 760px) {
    .row-main { grid-template-columns: 1fr auto; }
    .row-url { grid-column: 1 / -1; }
  }
</style>
