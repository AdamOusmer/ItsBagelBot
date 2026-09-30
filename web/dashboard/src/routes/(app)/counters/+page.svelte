<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Select } from '@bagel/ui/svelte';
  import { enhance, deserialize } from '$app/forms';
  import { goto, invalidateAll } from '$app/navigation';
  import { tick, untrack } from 'svelte';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    PageHead,
    PageToolbar,
    AlertBanner,
    DeckLayout,
    DeckList,
    EmptyState,
    InspectorSurface,
    Scroller,
    SearchInput,
    Button,
    Field,
    FieldError,
    EditorFooter,
    createDiscardGuard,
    ConfirmDialog,
    Icon,
    IconButton,
    Input,
    Label,
    Table,
    Tag,
    Text,
    toast,
    SegmentedControl
  } from '@bagel/ui/svelte';
  import {
    getI18n,
    COUNTER_SCOPES,
    type CounterDef,
    type CounterEntryView,
    type CounterScope,
    actionPayload,
    toastFailure,
    type ActionOk
  } from '@bagel/kit';
  import CounterRow from '$lib/components/counters/CounterRow.svelte';
  import { focusFirstInvalid } from '@bagel/ui/svelte';
  import { formatCounterValue, parseCounterValue } from '@bagel/kit/validation';

  let { data } = $props();
  const { t } = getI18n();
  const failed = toastFailure(toast, t);

  // svelte-ignore state_referenced_locally
  let items = $state<CounterDef[]>(data.counters ?? []);
  // svelte-ignore state_referenced_locally
  let seed = data.counters;
  $effect(() => {
    if (data.counters !== seed) {
      seed = data.counters;
      items = data.counters ?? [];
    }
  });

  let search = $state('');
  const scopeOptions = $derived([
    t('counters.filterAll'),
    ...COUNTER_SCOPES.map((s) => scopeTag[s]),
  ]);
  let scopeLabelPicked = $state(t('counters.filterAll'));
  const scopeFilter = $derived<CounterScope | 'all'>(
    COUNTER_SCOPES.find((s) => scopeTag[s] === scopeLabelPicked) ?? 'all',
  );
  const rows = $derived(
    items
      .filter((c) => c.name.toLowerCase().includes(search.toLowerCase()))
      .filter((c) => scopeFilter === 'all' || c.scope === scopeFilter)
      .toSorted((a, b) => a.name.localeCompare(b.name))
  );

  const scopeTag: Record<CounterScope, string> = {
    channel: t('counters.tagChannel'),
    viewer: t('counters.tagViewer'),
    command: t('counters.tagCommand'),
    viewer_command: t('counters.tagViewerCommand')
  };

  const scopeLabel: Record<CounterScope, string> = {
    channel: t('counters.scopeChannel'),
    viewer: t('counters.scopeViewer'),
    command: t('counters.scopeCommand'),
    viewer_command: t('counters.scopeViewerCommand')
  };

  async function postSet(
    name: string,
    value: string,
    target?: { viewerId?: string; command?: string }
  ): Promise<ActionOk | null> {
    const body = new FormData();
    body.set('name', name);
    body.set('value', String(value));
    if (target?.viewerId && target.viewerId !== '0') body.set('viewer_id', target.viewerId);
    if (target?.command) body.set('command', target.command);
    return fetch('?/set', { method: 'POST', body })
      .then(async (res) => actionPayload(deserialize(await res.text())) ?? null)
      .catch(() => null);
  }

  function focusSelect(node: HTMLElement) {
    const input = node.querySelector('input');
    input?.focus();
    input?.select();
  }

  const NEW = '__new__';
  type CounterDraft = { name: string; scope: CounterScope; value: string };
  const inspector = createInspector<CounterDraft>();
  let expanded = $state<string | null>(null);
  let draft = $state<CounterDraft | null>(null);
  let attempted = $state(false);
  let saving = $state(false);
  let createForm = $state<HTMLFormElement | null>(null);
  let setForm = $state<HTMLFormElement | null>(null);

  $effect(() => {
    const snap = draft ? { ...draft } : null;
    if (snap) untrack(() => inspector.edit(snap));
  });

  const creating = $derived(expanded === NEW);
  const canSave = $derived(creating || inspector.dirty);
  const selected = $derived(expanded && expanded !== NEW ? items.find((c) => c.name === expanded) : undefined);
  const entriesReady = $derived(!!selected && data.selected === selected.name);
  const nameError = $derived(
    creating && attempted && !normCounterName(draft?.name ?? '') ? t('counters.errName') : undefined
  );
  const valueError = $derived(
    selected?.scope === 'channel' && attempted && draft && parseCounterValue(draft.value) === null
      ? t('counters.errValue')
      : undefined
  );

  function normCounterName(raw: string): string {
    return raw.trim().replace(/^!/, '').toLowerCase().slice(0, 64);
  }

  async function focusNameError() {
    await tick();
    document.getElementById('counter-name')?.focus();
  }

  function present(id: string | null, next: CounterDraft | null) {
    expanded = id;
    attempted = false;
    renameError = '';
    addAttempted = false;
    if (id && next) {
      inspector.open(id, { ...next });
      draft = { ...next };
    } else {
      inspector.reset();
      draft = null;
    }
  }

  function clearSubFields() {
    renameValue = '';
    addUser = '';
    addCommand = '';
    addValue = '0';
  }

  const discard = createDiscardGuard(
    () =>
      inspector.dirty || renameValue.trim() !== '' || addUser.trim() !== '' || addCommand.trim() !== '',
    () => {
      clearSubFields();
      present(null, null);
    }
  );
  const guarded = discard.guard;

  function syncSelection(name: string | null) {
    if (name) void goto(`/counters?c=${encodeURIComponent(name)}`, { noScroll: true, keepFocus: true });
    else if (data.selected) void goto('/counters', { noScroll: true, keepFocus: true });
  }

  function openNew() {
    guarded(() => {
      clearSubFields();
      present(NEW, { name: '', scope: 'channel', value: '0' });
      syncSelection(null);
    });
  }

  function openCounter(c: CounterDef) {
    if (expanded === c.name) {
      closeEditor();
      return;
    }
    guarded(() => {
      clearSubFields();
      const channel = c.scope === 'channel';
      present(c.name, channel ? { name: c.name, scope: c.scope, value: c.value } : null);
      syncSelection(channel ? null : c.name);
    });
  }

  function closeEditor() {
    guarded(() => {
      present(null, null);
      syncSelection(null);
    });
  }

  function clearFilters() {
    search = '';
    scopeLabelPicked = t('counters.filterAll');
  }

  $effect(() => {
    if (expanded && expanded !== NEW && !items.some((c) => c.name === expanded)) {
      present(null, null);
    }
  });

  const inspectorTitle = $derived(
    expanded === NEW
      ? t('counters.newTitle')
      : selected && selected.scope !== 'channel'
        ? t('counters.entriesTitle', { name: selected.name })
        : (selected?.name ?? '')
  );

  function beginSaveRequest() {
    saving = true;
    return inspector.beginSave()?.requestId;
  }
  function endSaveRequest(requestId: string | undefined, ok: boolean): boolean {
    saving = false;
    return requestId ? inspector.resolved(requestId, { type: ok ? 'success' : 'error' }) : false;
  }

  const createSubmit: SubmitFunction = (input) => {
    attempted = true;
    if (!normCounterName(draft?.name ?? '')) {
      input.cancel();
      void focusNameError();
      return;
    }
    const requestId = beginSaveRequest();
    return async ({ result }) => {
      const ok = result.type === 'success' && actionPayload(result)?.ok === true;
      const applied = endSaveRequest(requestId, ok);
      if (!ok) {
        failed(actionPayload(result), 'counters.toastFailed');
        return;
      }
      toast('success', t('counters.toastCreated'));
      if (applied) present(null, null);
      await invalidateAll();
    };
  };

  const setSubmit: SubmitFunction = (input) => {
    attempted = true;
    if (!draft || parseCounterValue(draft.value) === null) {
      input.cancel();
      void focusFirstInvalid(setForm);
      return;
    }
    const requestId = beginSaveRequest();
    return async ({ result }) => {
      const ok = result.type === 'success' && actionPayload(result)?.ok === true;
      endSaveRequest(requestId, ok);
      if (!ok) {
        failed(actionPayload(result), 'counters.toastFailed');
        return;
      }
      toast('success', t('counters.toastSet'));
      await invalidateAll();
    };
  };

  let renameValue = $state('');
  let renameError = $state('');
  let renameForm = $state<HTMLFormElement | null>(null);
  let renaming = $state(false);
  const renameSubmit: SubmitFunction = (input) => {
    const target = expanded;
    const next = normCounterName(renameValue);
    renameError = !next ? t('counters.errName') : next === target ? t('counters.errRenameSame') : '';
    if (!next || next === target) {
      input.cancel();
      void focusFirstInvalid(document);
      return;
    }
    renaming = true;
    return async ({ result }) => {
      renaming = false;
      if (result.type === 'success' && actionPayload(result)?.ok) {
        toast('success', t('counters.toastRenamed'));
        clearSubFields();
        present(null, null);
        syncSelection(null);
        await invalidateAll();
        return;
      }
      failed(actionPayload(result), 'counters.toastFailed');
    };
  };

  let resetTarget = $state<CounterDef | null>(null);
  let resetForm = $state<HTMLFormElement | null>(null);
  let resetting = $state(false);
  const resetSubmit: SubmitFunction = () => {
    resetting = true;
    return async ({ result }) => {
      resetting = false;
      resetTarget = null;
      if (result.type === 'success' && actionPayload(result)?.ok) {
        toast('success', t('counters.toastReset'));
        await invalidateAll();
        return;
      }
      failed(actionPayload(result), 'counters.toastFailed');
    };
  };

  let addUser = $state('');
  let addCommand = $state('');
  let addValue = $state('0');
  let adding = $state(false);
  let addAttempted = $state(false);
  let addForm = $state<HTMLFormElement | null>(null);
  const addUserError = $derived(
    addAttempted && selected?.scope !== 'command' && !addUser.trim() ? t('counters.errAddUser') : undefined
  );
  const addCommandError = $derived(
    addAttempted && selected?.scope !== 'viewer' && !normCounterName(addCommand)
      ? t('counters.errAddCommand')
      : undefined
  );
  const addSubmit: SubmitFunction = (input) => {
    const scope = selected?.scope;
    addAttempted = true;
    const missingUser = scope !== 'command' && !addUser.trim();
    const missingCommand = scope !== 'viewer' && !normCounterName(addCommand);
    if (!scope || missingUser || missingCommand) {
      input.cancel();
      void focusFirstInvalid(addForm);
      return;
    }
    adding = true;
    return async ({ result }) => {
      adding = false;
      if (result.type === 'success' && actionPayload(result)?.ok) {
        toast('success', t('counters.toastAdded'));
        addUser = '';
        addCommand = '';
        addValue = '0';
        addAttempted = false;
        await invalidateAll();
        return;
      }
      const err = actionPayload(result)?.error;
      toast('danger', err === 'unknown_user' ? t('counters.errUnknownUser') : (err ?? t('counters.toastFailed')));
    };
  };

  let entryEdits = $state<Record<string, string>>({});
  let entrySaving = $state<string | null>(null);

  function entryKey(e: CounterEntryView): string {
    return e.viewerId + ':' + e.command;
  }

  function entryEditable(scope: CounterScope, e: CounterEntryView): boolean {
    return scope === 'command' ? e.command !== '' : e.viewerId !== '0';
  }

  function entryDraftValue(e: CounterEntryView): string | null {
    const raw = entryEdits[entryKey(e)];
    if (raw === undefined || raw.trim() === '') return null;
    return parseCounterValue(raw);
  }

  function entryInvalid(e: CounterEntryView): boolean {
    const raw = entryEdits[entryKey(e)];
    return raw !== undefined && raw.trim() !== '' && parseCounterValue(raw) === null;
  }

  const entryError = $derived(
    (data.entries ?? []).some(entryInvalid) ? t('counters.errValue') : ''
  );

  function entryDirty(e: CounterEntryView): boolean {
    const n = entryDraftValue(e);
    return n !== null && n !== e.value;
  }

  async function saveEntry(c: CounterDef, e: CounterEntryView) {
    const key = entryKey(e);
    const next = entryDraftValue(e);
    if (next === null || next === e.value || entrySaving !== null) return;
    entrySaving = key;
    const payload = await postSet(c.name, next, { viewerId: e.viewerId, command: e.command });
    entrySaving = null;
    if (payload?.ok) {
      toast('success', t('counters.toastSet'));
      delete entryEdits[key];
      await invalidateAll();
    } else {
      failed(payload, 'counters.toastFailed');
    }
  }

  function entryLabel(e: CounterEntryView): string {
    return e.viewerName || e.viewerLogin || e.command || e.viewerId;
  }

  let entryDeleteTarget = $state<CounterEntryView | null>(null);
  let entryDeleteForm = $state<HTMLFormElement | null>(null);
  let entryDeleting = $state(false);
  const entryDeleteSubmit: SubmitFunction = () => {
    entryDeleting = true;
    return async ({ result }) => {
      entryDeleting = false;
      entryDeleteTarget = null;
      if (result.type === 'success' && actionPayload(result)?.ok) {
        toast('success', t('counters.toastEntryRemoved'));
        await invalidateAll();
        return;
      }
      failed(actionPayload(result), 'counters.toastFailed');
    };
  };

  let deleteTarget = $state<CounterDef | null>(null);
  let deleteForm = $state<HTMLFormElement | null>(null);
  let deleting = $state(false);
  const deleteSubmit: SubmitFunction = () => {
    deleting = true;
    const target = deleteTarget;
    const snapshot = target ? { ...target } : null;
    if (target) {
      items = items.filter((c) => c.name !== target.name);
      if (expanded === target.name) present(null, null);
    }
    deleteTarget = null;
    return async ({ result }) => {
      deleting = false;
      if (result.type === 'success' && actionPayload(result)?.ok) {
        toast('success', t('counters.toastDeleted'));
        if (data.selected && snapshot && data.selected === snapshot.name) {
          await goto('/counters', { noScroll: true });
        }
        return;
      }
      if (snapshot) items = [...items.filter((c) => c.name !== snapshot.name), snapshot];
      failed(actionPayload(result), 'counters.toastFailed');
    };
  };
</script>

{#snippet footer(saveLabel: string)}
  <EditorFooter
    status={inspector.status}
    dirty={inspector.dirty}
    {canSave}
    {saveLabel}
    cancelLabel={t('common.cancel')}
    savingLabel={t('counters.saving')}
    savedLabel={t('counters.saved')}
    errorLabel={t('counters.toastFailed')}
    dirtyLabel={t('counters.unsavedChanges')}
    onCancel={closeEditor}
  />
{/snippet}

{#snippet secHead(label: string)}
  <div class="sec-head"><Label mono as="span">{label}</Label></div>
{/snippet}

{#snippet renameBlock()}
  <div class="rename-row">
    <Input
      fill
      name="new_name"
      form="counter-rename-form"
      placeholder={t('counters.renamePh')}
      aria-label={t('counters.rename')}
      required
      invalid={!!renameError}
      aria-invalid={renameError ? 'true' : undefined}
      aria-describedby={renameError ? 'counter-rename-err' : undefined}
      maxlength={64}
      bind:value={renameValue}
    />
    <Button variant="ghost" busy={renaming} onclick={() => renameForm?.requestSubmit()}>
      {t('counters.rename')}
    </Button>
  </div>
  <FieldError id="counter-rename-err" message={renameError} />
{/snippet}

<section class="screen active">
  <PageHead eyebrow={t('counters.eyebrow')} description={t('counters.description')}>
    {t('counters.titlePre')}<em>{t('counters.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('counters.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet leading()}
      <SegmentedControl
        options={scopeOptions}
        bind:value={scopeLabelPicked}
        label={t('counters.filterAria')}
      />
    {/snippet}
    {#snippet trailing()}
      <div class="toolbar-search">
        <SearchInput fill placeholder={t('counters.searchPlaceholder')} bind:value={search} debounceMs={200} />
      </div>
      <Button variant="primary" onclick={openNew} disabled={expanded === NEW}>
        {t('counters.create')}
      </Button>
    {/snippet}
  </PageToolbar>

  <DeckLayout inspecting={expanded !== null}>
    <DeckList>
      {#if rows.length}
        <div role="list" aria-label={t('counters.listTitle')}>
          {#each rows as c, i (c.name)}
            <CounterRow
              counter={c}
              index={i + 1}
              expanded={expanded === c.name}
              onExpand={() => openCounter(c)}
              onDelete={() => (deleteTarget = c)}
            />
          {/each}
        </div>
      {:else if items.length === 0}
        <EmptyState title={t('counters.emptyTitle')} body={t('counters.emptySub')}>
          <Button variant="primary" onclick={openNew}>{t('counters.create')}</Button>
        </EmptyState>
      {:else}
        <EmptyState title={t('counters.noneMatch')}>
          <Button variant="secondary" onclick={clearFilters}>{t('counters.clearFilters')}</Button>
        </EmptyState>
      {/if}
    </DeckList>

    {#if expanded !== null}
      <InspectorSurface
        open
        title={inspectorTitle}
        controls="counter-inspector"
        closeLabel={t('common.cancel')}
        onClose={closeEditor}
      >
        {#if creating && draft}
          <form
            method="POST"
            action="?/create"
            class="ins-form"
            novalidate
            use:enhance={createSubmit}
            bind:this={createForm}
          >
            <Scroller fill padding="16px" smooth>
              <Field label={t('counters.fieldName')} error={nameError} errorId="counter-name-err">
                <Input
                  id="counter-name"
                  name="name"
                  placeholder={t('counters.fieldNamePh')}
                  maxlength={64}
                  bind:value={draft.name}
                  invalid={!!nameError}
                  aria-invalid={nameError ? 'true' : undefined}
                  aria-describedby={nameError ? 'counter-name-err' : undefined}
                  required
                />
              </Field>

              <Field label={t('counters.fieldScope')}>
                <Select
                  fill
                  name="scope" bind:value={draft.scope}
                  options={COUNTER_SCOPES.map((s) => ({ value: s, label: scopeLabel[s] }))}
                />
              </Field>
              <div class="hints">
                <Text size="xs" tone="muted">{t('counters.scopeHint')}</Text>
                <Text size="xs" tone="muted">{t('counters.scopeLocked')}</Text>
              </div>
            </Scroller>
            {@render footer(t('counters.create'))}
          </form>
        {:else if selected?.scope === 'channel' && draft}
          <form
            method="POST"
            action="?/set"
            class="ins-form"
            novalidate
            use:enhance={setSubmit}
            bind:this={setForm}
          >
            <input type="hidden" name="name" value={selected.name} />
            <Scroller fill padding="16px" smooth>
              <div class="ins-sub"><Text size="xs" tone="muted">{scopeLabel[selected.scope]}</Text></div>

              <div class="sec">
                <Field label={t('counters.colValue')} error={valueError} errorId="counter-value-err">
                  <span class="big-num" use:focusSelect>
                    <Input
                      fill
                      type="text"
                      inputmode="numeric"
                      name="value"
                      bind:value={draft.value}
                      invalid={!!valueError}
                      aria-invalid={valueError ? 'true' : undefined}
                      aria-describedby={valueError ? 'counter-value-err' : undefined}
                    />
                  </span>
                </Field>
              </div>

              <div class="sec sec-util">
                {@render secHead(t('counters.rename'))}
                {@render renameBlock()}
              </div>
            </Scroller>
            {@render footer(t('counters.set'))}
          </form>
        {:else if selected}
          {@const showViewer = selected.scope !== 'command'}
          {@const showSource = selected.scope !== 'viewer'}
          <div class="ins-form">
            <Scroller fill padding="16px" smooth>
              <div class="ins-sub"><Text size="xs" tone="muted">{scopeLabel[selected.scope]}</Text></div>

              <div class="sec">
                <div class="sec-head">
                  <Label mono as="span">{t('counters.valuesTitle')}</Label>
                  {#if entriesReady && (data.entries ?? []).length}
                    <span class="sec-count"><Tag tone="bare">{(data.entries ?? []).length}</Tag></span>
                  {/if}
                </div>
                {#if !entriesReady}
                  <Text size="xs" tone="muted" role="status">{t('common.loading')}</Text>
                {:else if (data.entries ?? []).length === 0}
                  <Text size="xs" tone="muted">{t('counters.entriesEmpty')}</Text>
                {:else}
                  <Table label={t('counters.entriesTitle', { name: selected.name })}>
                    <caption class="bb-sr-only">{t('counters.entriesTitle', { name: selected.name })}</caption>
                    <thead>
                      <tr>
                        {#if showViewer}<th scope="col">{t('counters.colViewer')}</th>{/if}
                        {#if showSource}<th scope="col">{t('counters.colSource')}</th>{/if}
                        <th scope="col" class="r val-col">{t('counters.colValue')}</th>
                        <th scope="col" class="act-col"><span class="bb-sr-only">{t('counters.colActions')}</span></th>
                      </tr>
                    </thead>
                    <tbody>
                      {#each data.entries ?? [] as e (e.viewerId + ':' + e.command)}
                        <tr>
                          {#if showViewer}<th scope="row">{e.viewerName || e.viewerLogin || e.viewerId}</th>{/if}
                          {#if showSource}<td class="mut">{e.command || '·'}</td>{/if}
                          <td class="r val-col">
                            <span class="entry-edit">
                              {#if entryEditable(selected.scope, e)}
                                <Input
                                  align="end"
                                  inputmode="numeric"
                                  aria-label={t('counters.colValue')}
                                  invalid={entryInvalid(e)}
                                  aria-invalid={entryInvalid(e) ? 'true' : undefined}
                                  aria-describedby="counter-entry-err"
                                  value={entryEdits[entryKey(e)] ?? e.value}
                                  oninput={(ev: Event & { currentTarget: HTMLInputElement }) => (entryEdits[entryKey(e)] = ev.currentTarget.value)}
                                  onkeydown={(ev: KeyboardEvent) => {
                                    if (ev.key === 'Enter') void saveEntry(selected, e);
                                  }}
                                />
                                <span class="entry-check" class:off={!entryDirty(e)}>
                                  <IconButton
                                    size="sm"
                                    label={t('counters.set')}
                                    disabled={!entryDirty(e) || entrySaving !== null}
                                    onclick={() => saveEntry(selected, e)}
                                  ><Icon name="check" size={15} /></IconButton>
                                </span>
                              {:else}
                                <span class="entry-ro">{formatCounterValue(e.value)}</span>
                                <span class="entry-slot" aria-hidden="true"></span>
                              {/if}
                            </span>
                          </td>
                          <td class="act-col">
                            {#if entryEditable(selected.scope, e)}
                              <IconButton
                                size="sm"
                                label={t('counters.entryDeleteAria', { name: entryLabel(e) })}
                                onclick={() => (entryDeleteTarget = e)}
                                tone="danger"
                              ><Icon name="trash" size={15} /></IconButton>
                            {/if}
                          </td>
                        </tr>
                      {/each}
                    </tbody>
                  </Table>
                  <div class="entry-err"><FieldError id="counter-entry-err" message={entryError} /></div>
                {/if}
              </div>

              <div class="sec">
                {@render secHead(t('counters.addTitle'))}
                <form
                  method="POST"
                  action="?/addEntry"
                  class="add"
                  novalidate
                  use:enhance={addSubmit}
                  bind:this={addForm}
                >
                  <input type="hidden" name="name" value={selected.name} />
                  {#if showViewer}
                    <Field label={t('counters.addUser')} error={addUserError} errorId="counter-add-user-err">
                      <Input
                        name="username"
                        placeholder={t('counters.addUserPh')}
                        maxlength={32}
                        required
                        invalid={!!addUserError}
                        aria-invalid={addUserError ? 'true' : undefined}
                        aria-describedby={addUserError ? 'counter-add-user-err' : undefined}
                        bind:value={addUser}
                      />
                    </Field>
                  {/if}
                  {#if showSource}
                    <Field label={t('counters.addCommand')} error={addCommandError} errorId="counter-add-command-err">
                      <Input
                        name="command"
                        placeholder={t('counters.addCommandPh')}
                        maxlength={64}
                        required
                        invalid={!!addCommandError}
                        aria-invalid={addCommandError ? 'true' : undefined}
                        aria-describedby={addCommandError ? 'counter-add-command-err' : undefined}
                        bind:value={addCommand}
                      />
                    </Field>
                  {/if}
                  <div class="add-foot">
                    <div class="add-val">
                      <Field label={t('counters.colValue')}>
                        <Input fill type="text" inputmode="numeric" name="value" bind:value={addValue} />
                      </Field>
                    </div>
                    <Button variant="secondary" type="submit" busy={adding}>
                      {t('counters.add')}
                    </Button>
                  </div>
                </form>
              </div>

              <div class="sec sec-util">
                {@render secHead(t('counters.rename'))}
                {@render renameBlock()}
              </div>
            </Scroller>
            <div class="ins-foot">
              <Button variant="ghost" onclick={closeEditor}>{t('common.cancel')}</Button>
              <Button onclick={() => (resetTarget = selected)} tone="danger">
                {t('counters.reset')}
              </Button>
            </div>
          </div>
        {/if}
      </InspectorSurface>
    {/if}
  </DeckLayout>
</section>

<ConfirmDialog
  open={discard.open}
  title={t('counters.discardTitle')}
  body={t('counters.discardBody')}
  confirmLabel={t('counters.discard')}
  cancelLabel={t('counters.keepEditing')}
  onCancel={discard.cancel}
  onConfirm={discard.confirm}
  tone="danger"
/>

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('counters.deleteTitle')}
  body={t('counters.deleteBody', { name: deleteTarget?.name ?? '' })}
  confirmLabel={t('counters.del')}
  cancelLabel={t('common.cancel')}
  busy={deleting}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
  tone="danger"
/>
<form method="POST" action="?/delete" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="name" value={deleteTarget?.name ?? ''} />
</form>

<form
  id="counter-rename-form"
  method="POST"
  action="?/rename"
  use:enhance={renameSubmit}
  bind:this={renameForm}
  hidden
>
  <input type="hidden" name="name" value={selected?.name ?? ''} />
</form>

<ConfirmDialog
  open={resetTarget !== null}
  title={t('counters.resetTitle')}
  body={t('counters.resetBody', { name: resetTarget?.name ?? '' })}
  confirmLabel={t('counters.reset')}
  cancelLabel={t('common.cancel')}
  busy={resetting}
  onCancel={() => (resetTarget = null)}
  onConfirm={() => resetForm?.requestSubmit()}
  tone="danger"
/>
<form method="POST" action="?/set" use:enhance={resetSubmit} bind:this={resetForm} hidden>
  <input type="hidden" name="name" value={resetTarget?.name ?? ''} />
  <input type="hidden" name="value" value="0" />
</form>

<ConfirmDialog
  open={entryDeleteTarget !== null}
  title={t('counters.entryDeleteTitle')}
  body={t('counters.entryDeleteBody', { name: entryDeleteTarget ? entryLabel(entryDeleteTarget) : '' })}
  confirmLabel={t('counters.remove')}
  cancelLabel={t('common.cancel')}
  busy={entryDeleting}
  onCancel={() => (entryDeleteTarget = null)}
  onConfirm={() => entryDeleteForm?.requestSubmit()}
  tone="danger"
/>
<form method="POST" action="?/deleteEntry" use:enhance={entryDeleteSubmit} bind:this={entryDeleteForm} hidden>
  <input type="hidden" name="name" value={selected?.name ?? ''} />
  <input
    type="hidden"
    name="viewer_id"
    value={entryDeleteTarget && entryDeleteTarget.viewerId !== '0' ? entryDeleteTarget.viewerId : ''}
  />
  <input type="hidden" name="command" value={entryDeleteTarget?.command ?? ''} />
</form>

<style>
  .toolbar-search { width: 220px; max-width: 100%; }

  .rename-row { display: flex; align-items: center; gap: 8px; }

  .ins-form { display: flex; flex-direction: column; min-height: 0; flex: 1; }
  .ins-foot {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 10px;
    padding: 12px 16px;
    border-top: 1px solid var(--bb-border);
    background: var(--bb-bg-1);
    flex: none;
  }

  .entry-err { min-height: 22px; }

  .ins-sub { margin: 0 0 16px; }

  .sec { padding: 0 0 16px; }
  .sec + .sec { padding-top: 16px; border-top: 1px solid var(--bb-border); }
  .sec-util { --input-fs: var(--bb-text-xs); }

  .sec-head { display: flex; align-items: center; gap: 8px; margin: 0 0 10px; }
  .sec-count { display: inline-flex; font-variant-numeric: tabular-nums; }

  .big-num { display: block; max-width: 160px; --input-fs: var(--bb-text-md); }

  .hints { display: flex; flex-direction: column; gap: 8px; }

  .val-col { width: 128px; }
  .act-col { width: 32px; padding-left: 4px; padding-right: 0; text-align: right; }

  .entry-edit {
    --input-w: 90px;
    display: grid;
    grid-template-columns: minmax(0, 1fr) 28px;
    align-items: center;
    gap: 6px;
    justify-items: end;
  }
  .entry-ro {
    width: 90px;
    text-align: right;
    font-variant-numeric: tabular-nums;
    color: var(--bb-white);
  }
  .entry-slot { width: 28px; height: 28px; }
  .entry-check { display: inline-flex; }
  .entry-check.off { visibility: hidden; }

  .add { --field-mb: 12px; }
  .add-foot { display: flex; align-items: flex-end; gap: 10px; }
  .add-val { flex: none; width: 96px; --field-mb: 0; }

  @media (max-width: 760px) {
    .toolbar-search { width: 100%; }
  }
</style>
