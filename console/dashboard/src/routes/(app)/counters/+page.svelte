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
  import CounterRow from '$lib/components/counters/CounterRow.svelte';

  let { data } = $props();
  const { t } = getI18n();

  // Local source of truth, reseeded when a fresh SSR load lands. create / set /
  // delete resync through invalidateAll (which swaps `data` and re-seeds here).
  // The shape and the ?c= entries contract are exactly what the load returns.
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

  // postSet writes an absolute value for one stored bucket of an entry-scoped
  // counter (the inspector's per-entry edit), wrapping main's ?/set action.
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

  // focusSelect opens a field ready to overtype: the channel value editor's
  // whole point is correcting a number, so the current value lands focused and
  // selected. (Enter submits the surrounding form by default.)
  function focusSelect(node: HTMLInputElement) {
    node.focus();
    node.select();
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
  <!-- The input joins the hidden ?/rename form via form=, so it can sit inside
       the set form without nesting forms. The section head already says
       "Rename", so the field carries only its placeholder (sr-only label). -->
  <div class="rename-row">
    <input
      class="search rename-input"
      name="new_name"
      form="counter-rename-form"
      placeholder={t('counters.renamePh')}
      aria-label={t('counters.rename')}
      maxlength="64"
      bind:value={renameValue}
    />
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
        <div class="list" role="list" aria-label={t('counters.listTitle')}>
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
              <div class="hints">
                <p class="hint">{t('counters.scopeHint')}</p>
                <p class="hint">{t('counters.scopeLocked')}</p>
              </div>
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
              <p class="ins-sub">{scopeLabel[selected.scope]}</p>

              <div class="sec">
                <Field label={t('counters.colValue')}>
                  <input class="search num big-num" type="number" name="value" step="1" bind:value={setValue} use:focusSelect />
                </Field>
              </div>

              <div class="sec sec-util">
                <span class="sec-head">{t('counters.rename')}</span>
                {@render renameBlock()}
              </div>
            </Scroller>
            <div class="ins-foot">
              <Button variant="ghost" onclick={closeEditor}>{t('common.cancel')}</Button>
              <Button variant="primary" type="submit" icon="check" loading={setting}>
                {t('counters.set')}
              </Button>
            </div>
          </form>
        {:else if selected}
          {@const showViewer = selected.scope !== 'command'}
          {@const showSource = selected.scope !== 'viewer'}
          <div class="ins-form">
            <Scroller fill padding="16px">
              <p class="ins-sub">{scopeLabel[selected.scope]}</p>

              <!-- Values lead: the stored buckets are the point of this panel. -->
              <div class="sec">
                <div class="sec-head-row">
                  <span class="sec-head">{t('counters.valuesTitle')}</span>
                  {#if entriesReady && (data.entries ?? []).length}
                    <span class="sec-count">{(data.entries ?? []).length}</span>
                  {/if}
                </div>
                {#if !entriesReady}
                  <p class="hint" role="status">{t('common.loading')}</p>
                {:else if (data.entries ?? []).length === 0}
                  <p class="hint">{t('counters.entriesEmpty')}</p>
                {:else}
                  <div class="tbl-wrap">
                    <table class="tbl">
                      <caption class="sr-only">{t('counters.entriesTitle', { name: selected.name })}</caption>
                      <thead>
                        <tr>
                          {#if showViewer}<th scope="col">{t('counters.colViewer')}</th>{/if}
                          {#if showSource}<th scope="col">{t('counters.colSource')}</th>{/if}
                          <th scope="col" class="r">{t('counters.colValue')}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {#each data.entries ?? [] as e (e.viewerId + ':' + e.command)}
                          <tr>
                            {#if showViewer}<th scope="row">{e.viewerName || e.viewerLogin || e.viewerId}</th>{/if}
                            {#if showSource}<td class="mut">{e.command || '·'}</td>{/if}
                            <!-- Value cell is always a 2-track grid (number box |
                                 28px save slot) so the number's right edge is
                                 invariant: the save check toggles visibility, it
                                 is never inserted into or removed from the flow.
                                 Read-only buckets fill the same tracks. -->
                            <td class="r">
                              <span class="entry-edit">
                                {#if entryEditable(selected.scope, e)}
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
                                  <MiniButton
                                    icon="check"
                                    class={entryDirty(e) ? 'entry-check' : 'entry-check is-off'}
                                    aria-label={t('counters.set')}
                                    disabled={!entryDirty(e) || entrySaving !== null}
                                    onclick={() => saveEntry(selected, e)}
                                  />
                                {:else}
                                  <span class="entry-ro">{e.value.toLocaleString()}</span>
                                  <span class="entry-slot" aria-hidden="true"></span>
                                {/if}
                              </span>
                            </td>
                          </tr>
                        {/each}
                      </tbody>
                    </table>
                  </div>
                {/if}
              </div>

              <!-- Add a value: the username resolves to its Twitch id on save;
                   the counter's scope decides which key fields show. -->
              <div class="sec">
                <span class="sec-head">{t('counters.addTitle')}</span>
                <form method="POST" action="?/addEntry" class="add" novalidate use:enhance={addSubmit}>
                  <input type="hidden" name="name" value={selected.name} />
                  {#if showViewer}
                    <Field label={t('counters.addUser')}>
                      <input
                        class="search"
                        name="username"
                        placeholder={t('counters.addUserPh')}
                        maxlength="32"
                        bind:value={addUser}
                      />
                    </Field>
                  {/if}
                  {#if showSource}
                    <Field label={t('counters.addCommand')}>
                      <input
                        class="search"
                        name="command"
                        placeholder={t('counters.addCommandPh')}
                        maxlength="64"
                        bind:value={addCommand}
                      />
                    </Field>
                  {/if}
                  <div class="add-foot">
                    <div class="add-val">
                      <Field label={t('counters.colValue')}>
                        <input class="search num" type="number" name="value" step="1" bind:value={addValue} />
                      </Field>
                    </div>
                    <Button variant="secondary" type="submit" icon="plus" loading={adding}>
                      {t('counters.add')}
                    </Button>
                  </div>
                </form>
              </div>

              <!-- Rename is a rare utility; keep it out of the way at the bottom. -->
              <div class="sec sec-util">
                <span class="sec-head">{t('counters.rename')}</span>
                {@render renameBlock()}
              </div>
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

  .rename-row { display: flex; align-items: center; gap: 8px; }
  .rename-input { flex: 1; min-width: 0; }

  .deck { display: grid; grid-template-columns: minmax(0, 1fr); gap: 16px; align-items: start; }
  @media (min-width: 1080px) {
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 420px; }
  }

  .list { margin: 0; padding: 0; }
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

  /* Scope subtitle: the InspectorSurface title already names the counter, so
     the panel body opens with just the scope in plain language, not a second
     copy of the name. */
  .ins-sub {
    margin: 0 0 16px;
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-muted);
  }

  /* Sections: each block of the inspector, divided by a hairline so values,
     add and rename read as distinct groups rather than one form dump. */
  .sec { padding: 0 0 16px; }
  .sec + .sec { padding-top: 16px; border-top: 1px solid var(--rule, rgba(240, 236, 228, 0.08)); }
  /* The rename utility sits quieter than the sections above it. */
  .sec-util :global(.search) { font-size: 12.5px; }

  .sec-head {
    display: block;
    margin: 0 0 10px;
    font-family: var(--bb-font-body);
    font-size: 10.5px;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--bb-muted);
    font-weight: 600;
  }
  .sec-head-row { display: flex; align-items: center; gap: 8px; margin: 0 0 10px; }
  .sec-head-row .sec-head { margin: 0; }
  .sec-count {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-muted);
    border: 1px solid var(--bb-border);
    border-radius: 999px;
    padding: 1px 7px;
    font-variant-numeric: tabular-nums;
  }

  /* The channel value is the point of that panel, so it gets a larger box. */
  .big-num :global(.num),
  .ins-form :global(.big-num) { font-size: 16px; max-width: 160px; }

  .hint { margin: 0; font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }
  .hints { display: flex; flex-direction: column; gap: 8px; }

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
  /* Fixed value column so every value cell is the same box and the right edge
     never drifts with content. */
  .tbl th.r,
  .tbl td.r { width: 128px; }

  /* Per-entry value cell: a 2-track grid (number box | 28px save slot). The
     save check toggles visibility inside its always-reserved slot, so the
     number's position is invariant whether the row is dirty, clean or
     read-only. */
  .entry-edit {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 28px;
    align-items: center;
    gap: 6px;
    justify-items: end;
  }
  .tbl :global(.entry-num) {
    width: 90px;
    text-align: right;
    font-variant-numeric: tabular-nums;
    appearance: textfield;
    -moz-appearance: textfield;
  }
  .tbl :global(.entry-num)::-webkit-outer-spin-button,
  .tbl :global(.entry-num)::-webkit-inner-spin-button { -webkit-appearance: none; margin: 0; }
  .entry-ro {
    width: 90px;
    text-align: right;
    font-variant-numeric: tabular-nums;
    color: var(--bb-white);
  }
  .entry-slot { width: 28px; height: 28px; }
  .tbl :global(.entry-check.is-off) { visibility: hidden; }

  /* Add a value: key fields stack full-width (the panel is only 420px, so a
     side-by-side row would cramp), then the value + Add button share the last
     line, button riding the field baseline. */
  .add :global(.field) { margin-bottom: 12px; }
  .add-foot { display: flex; align-items: flex-end; gap: 10px; }
  .add-val { flex: none; }
  .add-val :global(.num) { width: 96px; }
  .add-foot :global(.btn) { margin-bottom: 2px; }

  @media (max-width: 760px) {
    .toolbar-search { width: 100%; }
  }
</style>
