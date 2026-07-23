<script lang="ts">
  import { enhance, deserialize } from '$app/forms';
  import { goto, invalidateAll } from '$app/navigation';
  import { tick } from 'svelte';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    PageHead,
    PageToolbar,
    AlertBanner,
    DeckList,
    EmptyState,
    InspectorSurface,
    Scroller,
    SearchInput,
    Button,
    Field,
    ConfirmDialog,
    MiniButton,
    toast,
    getI18n,
    COUNTER_SCOPES,
    type CounterDef,
    type CounterEntryView,
    type CounterScope
  } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';
  import CounterRow from '$lib/components/counters/CounterRow.svelte';

  let { data } = $props();
  const { t } = getI18n();

  // Local source of truth, reseeded when a fresh SSR load lands. Optimistic
  // stepper writes mutate this list without a reload; create/set/delete resync
  // through invalidateAll (which swaps `data` and re-seeds here). The shape and
  // the ?c= entries contract are exactly what +page.server.ts's load returns.
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

  type ActionResult = { ok?: boolean; error?: string };
  function payloadOf(result: { type: string; data?: unknown }): ActionResult | undefined {
    return result.type === 'success' || result.type === 'failure' ? (result.data as ActionResult) : undefined;
  }

  // --- Per-row save-state machine (mirrors the commands deck) -----------------
  let rowStatus = $state<Record<string, SaveState>>({});
  const statusTimers = new Map<string, ReturnType<typeof setTimeout>[]>();
  function clearTimers(name: string) {
    for (const h of statusTimers.get(name) ?? []) clearTimeout(h);
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

  // --- Search + scope filter + sorted rows ------------------------------------
  let search = $state('');
  let scopeFilter = $state<CounterScope | 'all'>('all');
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

  // Plain-language scope names for the create picker and the filter.
  const scopeLabel: Record<CounterScope, string> = {
    channel: t('counters.scopeChannel'),
    viewer: t('counters.scopeViewer'),
    command: t('counters.scopeCommand'),
    viewer_command: t('counters.scopeViewerCommand')
  };

  // --- Optimistic +/- stepper (channel scope), wrapping main's ?/set action ---
  // set is absolute server-side, so a +1 posts (value + 1). Optimistic locally,
  // rolled back with a toast on failure. target addresses one stored bucket of
  // an entry-scoped counter (the inspector's per-entry edit).
  async function postSet(
    name: string,
    value: number,
    target?: { viewerId?: string; command?: string }
  ): Promise<ActionResult | null> {
    const body = new FormData();
    body.set('name', name);
    body.set('value', String(value));
    if (target?.viewerId && target.viewerId !== '0') body.set('viewer_id', target.viewerId);
    if (target?.command) body.set('command', target.command);
    return fetch('?/set', { method: 'POST', body })
      .then(async (res) => {
        const r = deserialize(await res.text());
        return r.type === 'success' || r.type === 'failure' ? ((r.data as ActionResult | undefined) ?? null) : null;
      })
      .catch(() => null);
  }

  async function step(c: CounterDef, delta: number) {
    const before = c.value;
    const next = before + delta;
    items = items.map((x) => (x.name === c.name ? { ...x, value: next } : x));
    setStatus(c.name, 'saving');
    const payload = await postSet(c.name, next);
    if (payload?.ok) {
      ackSaved(c.name);
    } else {
      items = items.map((x) => (x.name === c.name ? { ...x, value: before } : x));
      flagError(c.name);
      toast('err', payload?.error ?? t('counters.toastFailed'));
    }
  }

  // --- Inspector: new counter, or an existing one's value / entries ----------
  const NEW = '__new__';
  let expanded = $state<string | null>(null);

  // Create draft.
  let newName = $state('');
  let newScope = $state<CounterScope>('channel');
  let creating = $state(false);
  let nameError = $state('');

  // Channel set-value draft.
  let setValue = $state(0);
  let setting = $state(false);

  const selected = $derived(expanded && expanded !== NEW ? items.find((c) => c.name === expanded) : undefined);
  // Entry-scoped counters load their per-viewer buckets from the server via ?c=;
  // ready once the SSR selection matches the row we opened.
  const entriesReady = $derived(!!selected && data.selected === selected.name);

  function normCounterName(raw: string): string {
    return raw.trim().replace(/^!/, '').toLowerCase().slice(0, 64);
  }

  async function focusNameError() {
    await tick();
    document.getElementById('counter-name')?.focus();
  }

  function openNew() {
    nameError = '';
    newName = '';
    newScope = 'channel';
    expanded = NEW;
    if (data.selected) void goto('/counters', { noScroll: true, keepFocus: true });
  }

  function openCounter(c: CounterDef) {
    if (expanded === c.name) {
      closeEditor();
      return;
    }
    nameError = '';
    renameValue = '';
    addUser = '';
    addCommand = '';
    addValue = 0;
    expanded = c.name;
    if (c.scope === 'channel') {
      setValue = c.value;
      if (data.selected) void goto('/counters', { noScroll: true, keepFocus: true });
    } else {
      void goto(`/counters?c=${encodeURIComponent(c.name)}`, { noScroll: true, keepFocus: true });
    }
  }

  function closeEditor() {
    expanded = null;
    nameError = '';
    if (data.selected) void goto('/counters', { noScroll: true, keepFocus: true });
  }

  // Close the editor if the counter it held vanished (e.g. deleted).
  $effect(() => {
    if (expanded && expanded !== NEW && !items.some((c) => c.name === expanded)) {
      expanded = null;
    }
  });

  const inspectorTitle = $derived(
    expanded === NEW
      ? t('counters.newTitle')
      : selected && selected.scope !== 'channel'
        ? t('counters.entriesTitle', { name: selected.name })
        : (selected?.name ?? '')
  );

  // --- Create (wraps main's ?/create action) ----------------------------------
  const createSubmit: SubmitFunction = (input) => {
    const norm = normCounterName(newName);
    nameError = norm ? '' : t('counters.errName');
    if (nameError) {
      input.cancel();
      void focusNameError();
      return;
    }
    creating = true;
    return async ({ result }) => {
      creating = false;
      if (result.type === 'success' && payloadOf(result)?.ok) {
        toast('ok', t('counters.toastCreated'));
        expanded = null;
        newName = '';
        newScope = 'channel';
        await invalidateAll();
        return;
      }
      // Keep the form open with the typed name; surface the reason.
      toast('err', payloadOf(result)?.error ?? t('counters.toastFailed'));
    };
  };

  // --- Set value on a channel counter (wraps main's ?/set action) -------------
  const setSubmit: SubmitFunction = () => {
    setting = true;
    return async ({ result }) => {
      setting = false;
      if (result.type === 'success' && payloadOf(result)?.ok) {
        toast('ok', t('counters.toastSet'));
        expanded = null;
        await invalidateAll();
        return;
      }
      toast('err', payloadOf(result)?.error ?? t('counters.toastFailed'));
    };
  };

  // --- Rename (any scope; the service moves the row and its buckets). The
  // visible input lives inside the inspector's own form, so it joins the
  // hidden rename form through the HTML form= attribute, like delete/reset. --
  let renameValue = $state('');
  let renameForm = $state<HTMLFormElement | null>(null);
  let renaming = $state(false);
  const renameSubmit: SubmitFunction = (input) => {
    const target = expanded;
    const next = normCounterName(renameValue);
    if (!next || next === target) {
      input.cancel();
      return;
    }
    renaming = true;
    return async ({ result }) => {
      renaming = false;
      if (result.type === 'success' && payloadOf(result)?.ok) {
        toast('ok', t('counters.toastRenamed'));
        renameValue = '';
        closeEditor();
        await invalidateAll();
        return;
      }
      toast('err', payloadOf(result)?.error ?? t('counters.toastFailed'));
    };
  };

  // --- Reset (entry scopes: main's ?/set with value 0 clears every bucket) -----
  let resetTarget = $state<CounterDef | null>(null);
  let resetForm = $state<HTMLFormElement | null>(null);
  let resetting = $state(false);
  const resetSubmit: SubmitFunction = () => {
    resetting = true;
    return async ({ result }) => {
      resetting = false;
      resetTarget = null;
      if (result.type === 'success' && payloadOf(result)?.ok) {
        toast('ok', t('counters.toastReset'));
        await invalidateAll();
        return;
      }
      toast('err', payloadOf(result)?.error ?? t('counters.toastFailed'));
    };
  };

  // --- Manual bucket add (entry scopes; wraps ?/addEntry) ---------------------
  // The typed username resolves to its Twitch id server-side; the scope decides
  // which fields the form shows (viewer, command, or both).
  let addUser = $state('');
  let addCommand = $state('');
  let addValue = $state(0);
  let adding = $state(false);
  const addSubmit: SubmitFunction = (input) => {
    const scope = selected?.scope;
    const missing = scope === 'command' ? !normCounterName(addCommand) : !addUser.trim();
    if (!scope || missing) {
      input.cancel();
      return;
    }
    adding = true;
    return async ({ result }) => {
      adding = false;
      if (result.type === 'success' && payloadOf(result)?.ok) {
        toast('ok', t('counters.toastAdded'));
        addUser = '';
        addCommand = '';
        addValue = 0;
        await invalidateAll();
        return;
      }
      const err = payloadOf(result)?.error;
      toast('err', err === 'unknown_user' ? t('counters.errUnknownUser') : (err ?? t('counters.toastFailed')));
    };
  };

  // --- Per-entry value edit (entry scopes; wraps ?/set with a bucket target) --
  // Drafts are keyed by (viewer, command) bucket and hold the raw input text;
  // a row is dirty once the parsed draft differs from the stored value. Saving
  // posts a targeted set and resyncs through invalidateAll.
  let entryEdits = $state<Record<string, string>>({});
  let entrySaving = $state<string | null>(null);

  function entryKey(e: CounterEntryView): string {
    return e.viewerId + ':' + e.command;
  }

  // A bucket is editable only when the service can address it: the command
  // bucket for command scope, the viewer for the viewer scopes. An untargeted
  // set would reset the whole counter, so unaddressable rows stay read-only.
  function entryEditable(scope: CounterScope, e: CounterEntryView): boolean {
    return scope === 'command' ? e.command !== '' : e.viewerId !== '0';
  }

  function entryDraftValue(e: CounterEntryView): number | null {
    const raw = entryEdits[entryKey(e)];
    if (raw === undefined || raw.trim() === '') return null;
    const n = Math.trunc(Number(raw));
    return Number.isFinite(n) ? n : null;
  }

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
      toast('ok', t('counters.toastSet'));
      delete entryEdits[key];
      await invalidateAll();
    } else {
      toast('err', payload?.error ?? t('counters.toastFailed'));
    }
  }

  // --- Delete (optimistic removal, confirmed + named; wraps ?/delete) ----------
  let deleteTarget = $state<CounterDef | null>(null);
  let deleteForm = $state<HTMLFormElement | null>(null);
  let deleting = $state(false);
  const deleteSubmit: SubmitFunction = () => {
    deleting = true;
    const target = deleteTarget;
    const snapshot = target ? { ...target } : null;
    if (target) {
      items = items.filter((c) => c.name !== target.name);
      if (expanded === target.name) expanded = null;
    }
    deleteTarget = null;
    return async ({ result }) => {
      deleting = false;
      if (result.type === 'success' && payloadOf(result)?.ok) {
        toast('ok', t('counters.toastDeleted'));
        if (data.selected && snapshot && data.selected === snapshot.name) {
          await goto('/counters', { noScroll: true });
        }
        return;
      }
      if (snapshot) items = [...items.filter((c) => c.name !== snapshot.name), snapshot];
      toast('err', payloadOf(result)?.error ?? t('counters.toastFailed'));
    };
  };
</script>

{#snippet renameBlock()}
  <!-- The input joins the hidden ?/rename form via form=, so it can sit
       inside the set form without nesting forms. The button stays outside
       Field so the label wraps exactly one control. -->
  <div class="rename-row">
    <div class="rename-field">
      <Field label={t('counters.rename')}>
        <input
          class="search"
          name="new_name"
          form="counter-rename-form"
          placeholder={t('counters.renamePh')}
          maxlength="64"
          bind:value={renameValue}
        />
      </Field>
    </div>
    <Button variant="ghost" loading={renaming} onclick={() => renameForm?.requestSubmit()}>
      {t('counters.rename')}
    </Button>
  </div>
{/snippet}

<section class="screen active">
  <PageHead eyebrow={t('counters.eyebrow')} description={t('counters.description')}>
    {t('counters.titlePre')}<em>{t('counters.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('counters.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      <div class="chip-row" role="group" aria-label={t('counters.filterAria')}>
        <button class="chip {scopeFilter === 'all' ? 'on' : ''}" onclick={() => (scopeFilter = 'all')}>
          {t('counters.filterAll')}
        </button>
        {#each COUNTER_SCOPES as s}
          <button class="chip {scopeFilter === s ? 'on' : ''}" onclick={() => (scopeFilter = s)}>{scopeTag[s]}</button>
        {/each}
      </div>
    {/snippet}
    {#snippet trail()}
      <div class="toolbar-search">
        <SearchInput placeholder={t('counters.searchPlaceholder')} bind:value={search} debounceMs={200} />
      </div>
      <Button variant="primary" icon="plus" onclick={openNew} disabled={expanded === NEW}>
        {t('counters.create')}
      </Button>
    {/snippet}
  </PageToolbar>

  <!-- The deck: full-width ledger list until a selection opens the docked
       inspector (no idle panel reserving a third of the row). -->
  <div class="deck {expanded !== null ? 'inspecting' : ''}">
    <DeckList>
      {#if rows.length}
        <ul class="list" aria-label={t('counters.listTitle')}>
          {#each rows as c, i (c.name)}
            <CounterRow
              counter={c}
              index={i + 1}
              status={rowStatus[c.name] ?? 'idle'}
              expanded={expanded === c.name}
              onExpand={() => openCounter(c)}
              onDelete={() => (deleteTarget = c)}
              onIncrement={() => step(c, 1)}
              onDecrement={() => step(c, -1)}
            />
          {/each}
        </ul>
      {:else if items.length === 0}
        <EmptyState icon="list" title={t('counters.emptyTitle')} body={t('counters.emptySub')}>
          <Button variant="primary" icon="plus" onclick={openNew}>{t('counters.create')}</Button>
        </EmptyState>
      {:else}
        <EmptyState icon="list" title={t('counters.noneMatch')} />
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
        {#if expanded === NEW}
          <form method="POST" action="?/create" class="ins-form" novalidate use:enhance={createSubmit}>
            <Scroller fill padding="16px">
              <Field label={t('counters.fieldName')}>
                <input
                  id="counter-name"
                  class="search"
                  name="name"
                  placeholder={t('counters.fieldNamePh')}
                  maxlength="64"
                  bind:value={newName}
                  aria-invalid={nameError ? 'true' : undefined}
                  aria-describedby={nameError ? 'counter-name-err' : undefined}
                  required
                />
              </Field>
              {#if nameError}
                <small id="counter-name-err" class="field-err" role="alert">{nameError}</small>
              {/if}

              <Field label={t('counters.fieldScope')}>
                <select class="search" name="scope" bind:value={newScope}>
                  {#each COUNTER_SCOPES as s}
                    <option value={s}>{scopeLabel[s]}</option>
                  {/each}
                </select>
              </Field>
              <p class="hint">{t('counters.scopeHint')}</p>
              <p class="hint">{t('counters.scopeLocked')}</p>
            </Scroller>
            <div class="ins-foot">
              <Button variant="ghost" onclick={closeEditor}>{t('common.cancel')}</Button>
              <Button variant="primary" type="submit" icon="plus" loading={creating}>
                {t('counters.create')}
              </Button>
            </div>
          </form>
        {:else if selected?.scope === 'channel'}
          <form method="POST" action="?/set" class="ins-form" novalidate use:enhance={setSubmit}>
            <input type="hidden" name="name" value={selected.name} />
            <Scroller fill padding="16px">
              <div class="identity">
                <span class="id-name">{selected.name}</span>
                <span class="id-tag">{scopeTag[selected.scope]}</span>
              </div>
              <Field label={t('counters.colValue')}>
                <input class="search num" type="number" name="value" step="1" bind:value={setValue} />
              </Field>
              {@render renameBlock()}
            </Scroller>
            <div class="ins-foot">
              <Button variant="ghost" onclick={closeEditor}>{t('common.cancel')}</Button>
              <Button variant="primary" type="submit" icon="check" loading={setting}>
                {t('counters.set')}
              </Button>
            </div>
          </form>
        {:else if selected}
          <div class="ins-form">
            <Scroller fill padding="16px">
              <div class="identity">
                <span class="id-name">{selected.name}</span>
                <span class="id-tag">{scopeTag[selected.scope]}</span>
              </div>
              {@render renameBlock()}

              <!-- Manual bucket add: the username resolves to its Twitch id on
                   save; the counter's scope decides which key fields show. -->
              <form method="POST" action="?/addEntry" class="add-form" novalidate use:enhance={addSubmit}>
                <input type="hidden" name="name" value={selected.name} />
                <div class="add-row">
                  {#if selected.scope !== 'command'}
                    <div class="add-field">
                      <Field label={t('counters.addUser')}>
                        <input
                          class="search"
                          name="username"
                          placeholder={t('counters.addUserPh')}
                          maxlength="32"
                          bind:value={addUser}
                        />
                      </Field>
                    </div>
                  {/if}
                  {#if selected.scope !== 'viewer'}
                    <div class="add-field">
                      <Field label={t('counters.addCommand')}>
                        <input
                          class="search"
                          name="command"
                          placeholder={t('counters.addCommandPh')}
                          maxlength="64"
                          bind:value={addCommand}
                        />
                      </Field>
                    </div>
                  {/if}
                  <div class="add-val">
                    <Field label={t('counters.colValue')}>
                      <input class="search num" type="number" name="value" step="1" bind:value={addValue} />
                    </Field>
                  </div>
                  <Button variant="ghost" type="submit" icon="plus" loading={adding}>
                    {t('counters.add')}
                  </Button>
                </div>
              </form>

              <p class="hint">{t('counters.resetHint')}</p>
              {#if !entriesReady}
                <p class="hint" role="status">{t('common.loading')}</p>
              {:else if (data.entries ?? []).length === 0}
                <p class="hint">{t('counters.entriesEmpty')}</p>
              {:else}
                <!-- Command scope pools every viewer, so its buckets have no
                     viewer column; the viewer scopes lead with it. -->
                <div class="tbl-wrap">
                  <table class="tbl">
                    <caption class="sr-only">{t('counters.entriesTitle', { name: selected.name })}</caption>
                    <thead>
                      <tr>
                        {#if selected.scope !== 'command'}
                          <th scope="col">{t('counters.colViewer')}</th>
                        {/if}
                        <th scope="col">{t('counters.colSource')}</th>
                        <th scope="col" class="r">{t('counters.colValue')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {#each data.entries ?? [] as e (e.viewerId + ':' + e.command)}
                        <tr>
                          {#if selected.scope !== 'command'}
                            <th scope="row">{e.viewerName || e.viewerLogin || e.viewerId}</th>
                            <td class="mut">{e.command || '·'}</td>
                          {:else}
                            <th scope="row">{e.command || '·'}</th>
                          {/if}
                          <td class="r">
                            {#if entryEditable(selected.scope, e)}
                              <span class="entry-edit">
                                <input
                                  class="search num entry-num"
                                  type="number"
                                  step="1"
                                  aria-label={t('counters.colValue')}
                                  value={entryEdits[entryKey(e)] ?? e.value}
                                  oninput={(ev) => (entryEdits[entryKey(e)] = ev.currentTarget.value)}
                                  onkeydown={(ev) => {
                                    if (ev.key === 'Enter') void saveEntry(selected, e);
                                  }}
                                />
                                {#if entryDirty(e)}
                                  <MiniButton
                                    icon="check"
                                    aria-label={t('counters.set')}
                                    disabled={entrySaving !== null}
                                    onclick={() => saveEntry(selected, e)}
                                  />
                                {/if}
                              </span>
                            {:else}
                              {e.value.toLocaleString()}
                            {/if}
                          </td>
                        </tr>
                      {/each}
                    </tbody>
                  </table>
                </div>
              {/if}
            </Scroller>
            <div class="ins-foot">
              <Button variant="ghost" onclick={closeEditor}>{t('common.cancel')}</Button>
              <Button variant="destructive" icon="trash" onclick={() => (resetTarget = selected)}>
                {t('counters.reset')}
              </Button>
            </div>
          </div>
        {/if}
      </InspectorSurface>
    {/if}
  </div>
</section>

<!-- Delete: confirmed + named; posts through a hidden form so the confirm owns
     the destructive click. -->
<ConfirmDialog
  open={deleteTarget !== null}
  title={t('counters.deleteTitle')}
  body={t('counters.deleteBody', { name: deleteTarget?.name ?? '' })}
  confirmLabel={t('counters.del')}
  cancelLabel={t('common.cancel')}
  danger
  busy={deleting}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/delete" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="name" value={deleteTarget?.name ?? ''} />
</form>

<!-- Rename: the visible input in the inspector belongs to this form via form=. -->
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

<!-- Reset an entry-scoped counter: value 0 clears every stored bucket. -->
<ConfirmDialog
  open={resetTarget !== null}
  title={t('counters.resetTitle')}
  body={t('counters.resetBody', { name: resetTarget?.name ?? '' })}
  confirmLabel={t('counters.reset')}
  cancelLabel={t('common.cancel')}
  danger
  busy={resetting}
  onCancel={() => (resetTarget = null)}
  onConfirm={() => resetForm?.requestSubmit()}
/>
<form method="POST" action="?/set" use:enhance={resetSubmit} bind:this={resetForm} hidden>
  <input type="hidden" name="name" value={resetTarget?.name ?? ''} />
  <input type="hidden" name="value" value="0" />
</form>

<style>
  .toolbar-search { width: 220px; max-width: 100%; }
  .toolbar-search :global(.si) { width: 100%; }

  .rename-row { display: flex; align-items: flex-end; gap: 8px; }
  .rename-field { flex: 1; min-width: 0; }

  .deck { display: grid; grid-template-columns: minmax(0, 1fr); gap: 16px; align-items: start; }
  @media (min-width: 1080px) {
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 420px; }
  }

  .list { list-style: none; margin: 0; padding: 0; }
  .list :global(.row-shell:last-child) { border-bottom: none; }

  /* The inspector body fills the surface below its head: the Scroller scrolls and
     the footer stays pinned at the bottom. */
  .ins-form { display: flex; flex-direction: column; min-height: 0; flex: 1; }
  .ins-form :global(.num) { max-width: 200px; }
  .ins-foot {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 10px;
    padding: 12px 16px;
    border-top: 1px solid var(--rule);
    background: var(--bb-bg-1, #111);
    flex: none;
  }

  .field-err {
    display: block;
    margin: -8px 0 12px;
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    color: #cf8a78;
  }

  .identity { display: flex; align-items: center; gap: 10px; margin: 0 0 14px; }
  .id-name { font-family: var(--bb-font-display); font-weight: 700; font-size: 15px; color: var(--bb-white); }
  .id-tag {
    font-family: var(--bb-font-body);
    font-size: 11px;
    color: var(--bb-muted);
    border: 1px solid var(--bb-border);
    border-radius: 999px;
    padding: 2px 8px;
  }

  .hint { margin: 0 0 12px; font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  .tbl-wrap { overflow-x: auto; -webkit-overflow-scrolling: touch; }
  .tbl { width: 100%; border-collapse: collapse; font-family: var(--bb-font-body); font-size: 13px; }
  .tbl th[scope='col'] {
    text-align: left;
    font-size: 11px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-muted);
    padding: 4px 8px;
    border-bottom: 1px solid var(--bb-border);
    font-weight: 600;
  }
  .tbl td,
  .tbl th[scope='row'] { padding: 7px 8px; border-bottom: 1px solid rgba(240, 236, 228, 0.05); color: var(--bb-white); }
  .tbl th[scope='row'] { text-align: left; font-weight: 600; }
  .tbl .r { text-align: right; }
  .tbl .mut { color: var(--bb-muted); }

  /* Per-entry edit: a compact right-aligned input; the save check only
     appears once the draft differs from the stored value. */
  .entry-edit { display: inline-flex; align-items: center; justify-content: flex-end; gap: 6px; }
  .tbl :global(.entry-num) { width: 90px; text-align: right; }

  /* Manual bucket add: key fields flex, the value stays compact, the button
     rides the baseline like the rename row. */
  .add-form { margin: 0 0 14px; }
  .add-row { display: flex; align-items: flex-end; gap: 8px; flex-wrap: wrap; }
  .add-field { flex: 1; min-width: 130px; }
  .add-val :global(.num) { width: 90px; }

  @media (max-width: 760px) {
    .toolbar-search { width: 100%; }
  }
</style>
