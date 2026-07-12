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
    toast,
    getI18n,
    COUNTER_SCOPES,
    type CounterDef,
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

  // --- Search + sorted rows ---------------------------------------------------
  let search = $state('');
  const rows = $derived(
    items
      .filter((c) => c.name.toLowerCase().includes(search.toLowerCase()))
      .toSorted((a, b) => a.name.localeCompare(b.name))
  );

  const scopeTag: Record<CounterScope, string> = {
    channel: t('counters.tagChannel'),
    viewer: t('counters.tagViewer'),
    viewer_command: t('counters.tagViewerCommand')
  };

  // --- Optimistic +/- stepper (channel scope), wrapping main's ?/set action ---
  // set is absolute server-side, so a +1 posts (value + 1). Optimistic locally,
  // rolled back with a toast on failure.
  async function postSet(name: string, value: number): Promise<ActionResult | null> {
    const body = new FormData();
    body.set('name', name);
    body.set('value', String(value));
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

<section class="screen active">
  <PageHead eyebrow={t('counters.eyebrow')} description={t('counters.description')}>
    {t('counters.titlePre')}<em>{t('counters.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('counters.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      <div class="toolbar-search">
        <SearchInput placeholder={t('counters.searchPlaceholder')} bind:value={search} debounceMs={200} />
      </div>
    {/snippet}
    {#snippet trail()}
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
                    <option value={s}>
                      {s === 'channel'
                        ? t('counters.scopeChannel')
                        : s === 'viewer'
                          ? t('counters.scopeViewer')
                          : t('counters.scopeViewerCommand')}
                    </option>
                  {/each}
                </select>
              </Field>
              <p class="hint">{t('counters.scopeHint')}</p>
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
              <p class="hint">{t('counters.resetHint')}</p>
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
                        <th scope="col">{t('counters.colViewer')}</th>
                        <th scope="col">{t('counters.colSource')}</th>
                        <th scope="col" class="r">{t('counters.colValue')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {#each data.entries ?? [] as e (e.viewerId + ':' + e.command)}
                        <tr>
                          <th scope="row">{e.viewerLogin || e.viewerId}</th>
                          <td class="mut">{e.command || '—'}</td>
                          <td class="r">{e.value.toLocaleString()}</td>
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

  @media (max-width: 760px) {
    .toolbar-search { width: 100%; }
  }
</style>
