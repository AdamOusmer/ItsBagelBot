<script lang="ts">
  import { enhance, deserialize } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Button,
    PageHead,
    Scroller,
    ConfirmDialog,
    EditorFooter,
    InspectorSurface,
    toast,
    getI18n,
    blankTimer,
    type TimerDef,
    MasterToggle,
    PageToolbar,
    AlertBanner,
    DeckList,
    EmptyState
  } from '@bagel/shared';
  import { untrack } from 'svelte';
  import { createInspector } from '$lib/inspector/inspector.svelte';
  import TimerRow from '$lib/components/timers/TimerRow.svelte';
  import TimerEditor from '$lib/components/timers/TimerEditor.svelte';

  let { data } = $props();
  const { t } = getI18n();

  // Local list, reseeded when a fresh SSR load lands (the /events invalidation
  // stream re-runs the loader after every confirmed write).
  // svelte-ignore state_referenced_locally
  let timers = $state<TimerDef[]>(data.timers ?? []);
  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      timers = data.timers ?? [];
      enabled = data.enabled ?? false;
    }
  });

  const rows = $derived(timers.toSorted((a, b) => a.intervalSeconds - b.intervalSeconds));

  // --- Inspector (shared controller over the pure state machine) --------------
  const NEW = '__new__';
  const inspector = createInspector<TimerDef>();
  // The editor binds this draft; edits flow into the machine for dirty tracking.
  let draft = $state<TimerDef | null>(null);
  let busy = $state(false);

  // Push editor changes into the machine for dirty tracking. The spread reads
  // each field so the effect re-runs on any field mutation; the edit itself is
  // untracked because it both reads and writes the machine's state, which would
  // otherwise make the effect depend on state it also mutates (an unsafe cycle).
  $effect(() => {
    const snap = draft ? { ...draft } : null;
    if (snap) untrack(() => inspector.edit(snap));
  });

  const creating = $derived(inspector.selectedId === NEW);
  // Enforce the server's clamp range (60s–24h) before submitting, so the saved
  // snapshot equals what the server stores and "Saved" is never shown for a
  // value the server silently normalized. Mirrors clampInt in +page.server.ts.
  const canSave = $derived(
    inspector.dirty &&
      !!draft &&
      draft.message.trim().length > 0 &&
      Number.isFinite(draft.intervalSeconds) &&
      draft.intervalSeconds >= 60 &&
      draft.intervalSeconds <= 86_400
  );

  // --- Dirty guard: every close/switch/new routes through one confirmation ----
  let discardOpen = $state(false);
  let afterDiscard: (() => void) | null = null;

  function guarded(action: () => void) {
    if (inspector.dirty) {
      afterDiscard = action;
      discardOpen = true;
    } else {
      action();
    }
  }
  function confirmDiscard() {
    discardOpen = false;
    inspector.reset();
    draft = null;
    const a = afterDiscard;
    afterDiscard = null;
    a?.();
  }
  function cancelDiscard() {
    discardOpen = false;
    afterDiscard = null;
  }

  function openNew() {
    guarded(() => {
      const b = blankTimer();
      inspector.open(NEW, b);
      draft = { ...b };
    });
  }
  function openEdit(tmr: TimerDef) {
    if (inspector.selectedId === tmr.id) {
      closeInspector();
      return;
    }
    guarded(() => {
      inspector.open(tmr.id, { ...tmr });
      draft = { ...tmr };
    });
  }
  function closeInspector() {
    guarded(() => {
      inspector.reset();
      draft = null;
    });
  }

  type ActionResult = { ok?: boolean; error?: string };
  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }
  function failed(payload: ActionResult | undefined, fallbackKey: string) {
    toast('err', payload?.error ?? t(fallbackKey));
  }

  // --- Save: immutable snapshot + request id; a late response can't cross rows -
  const saveSubmit: SubmitFunction = () => {
    const started = inspector.beginSave();
    const requestId = started?.requestId;
    const wasCreating = creating;
    busy = true;
    return async ({ result }) => {
      busy = false;
      const payload = payloadOf(result);
      const ok = result.type === 'success' && payload?.ok === true;
      // applied is false when the selection moved on during the request, so a
      // late response for one timer never mutates another's editor.
      const applied = requestId ? inspector.resolved(requestId, { type: ok ? 'success' : 'error' }) : false;
      if (ok) {
        toast('ok', t(wasCreating ? 'timers.toastCreated' : 'timers.toastSaved'));
        // A create has no client-side id to keep editing, so it closes — but only
        // if this response still owns the open editor. An update stays open and
        // clean (Save does not close the inspector).
        if (wasCreating && applied) {
          inspector.reset();
          draft = null;
        }
        await invalidateAll();
      } else {
        failed(payload, 'timers.toastSaveFailed');
      }
    };
  };

  // --- Row quick toggle (pause/resume, optimistic) ----------------------------
  const toggleSubmit =
    (tmr: TimerDef): SubmitFunction =>
    () => {
      const was = tmr.enabled;
      timers = timers.map((x) => (x.id === tmr.id ? { ...x, enabled: !was } : x));
      return async ({ result }) => {
        const payload = payloadOf(result);
        if (result.type === 'success' && payload?.ok) return;
        timers = timers.map((x) => (x.id === tmr.id ? { ...x, enabled: was } : x));
        failed(payload, 'timers.toastToggleFailed');
      };
    };

  // --- Delete: optimistic remove + Undo (timers are locally recreatable) ------
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

  async function onDelete(tmr: TimerDef) {
    timers = timers.filter((x) => x.id !== tmr.id);
    if (inspector.selectedId === tmr.id) {
      inspector.reset();
      draft = null;
    }
    const fd = new FormData();
    fd.set('id', tmr.id);
    const res = await postAction('delete', fd);
    if (!res?.ok) {
      timers = [...timers, tmr];
      failed(res ?? undefined, 'timers.toastDeleteFailed');
      return;
    }
    toast('ok', t('timers.toastDeleted'), {
      undoLabel: t('timers.undo'),
      onUndo: () => undoDelete(tmr)
    });
  }
  async function undoDelete(tmr: TimerDef) {
    const fd = new FormData();
    fd.set('timer', JSON.stringify(tmr));
    const res = await postAction('create', fd);
    if (res?.ok) await invalidateAll();
    else failed(res ?? undefined, 'timers.toastDeleteFailed');
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('timers.eyebrow')} description={t('timers.description')}>
    {t('timers.titlePre')}<em>{t('timers.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('timers.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      <MasterToggle
        action="?/toggle"
        bind:enabled
        label={t('timers.botOn')}
        hint={t('timers.botOnHint')}
        ariaLabel={t('timers.botOn')}
        failMessage={t('timers.toastToggleFailed')}
      />
    {/snippet}
    {#snippet trail()}
      <Button variant="primary" icon="plus" onclick={openNew} disabled={creating}>
        {t('timers.newTimer')}
      </Button>
    {/snippet}
  </PageToolbar>

  <!-- The deck: full-width ledger list until a selection opens the docked
       inspector (no idle panel reserving a third of the row). -->
  <div class="deck {inspector.isOpen ? 'inspecting' : ''}">
    <DeckList>
      <div class="list">
        {#each rows as tmr, i (tmr.id)}
          <TimerRow
            timer={tmr}
            index={i + 1}
            expanded={inspector.selectedId === tmr.id}
            onExpand={() => openEdit(tmr)}
            onDelete={() => onDelete(tmr)}
            toggleSubmit={toggleSubmit(tmr)}
          />
        {/each}
        {#if rows.length === 0}
          <EmptyState icon="clock" title={t('timers.emptyTitle')} body={t('timers.emptySub')}>
            <Button variant="primary" icon="plus" onclick={openNew}>{t('timers.newTimer')}</Button>
          </EmptyState>
        {/if}
      </div>
    </DeckList>

    {#if inspector.isOpen && draft}
      <InspectorSurface
        open
        title={creating ? t('timers.newTimer') : t('timers.editing')}
        controls="timer-editor"
        closeLabel={t('common.cancel')}
        onClose={closeInspector}
      >
        <form method="POST" action={creating ? '?/create' : '?/update'} novalidate use:enhance={saveSubmit} class="inspector-form">
          <input type="hidden" name="timer" value={JSON.stringify(draft)} />
          <Scroller fill padding="16px" data-lenis-prevent>
            <!-- Keyed on the selection so switching timers mounts a FRESH editor;
                 TimerEditor snapshots minutes from the draft at mount, and reusing
                 one instance would write the previous timer's interval into the
                 new draft. -->
            {#key inspector.selectedId}
              <TimerEditor bind:draft />
            {/key}
          </Scroller>
          <EditorFooter
            status={inspector.status}
            dirty={inspector.dirty}
            {canSave}
            saveLabel={creating ? t('timers.create') : t('timers.saveChanges')}
            cancelLabel={t('common.cancel')}
            savingLabel={t('timers.saving')}
            savedLabel={t('timers.saved')}
            errorLabel={t('timers.toastSaveFailed')}
            dirtyLabel={t('timers.unsavedChanges')}
            onCancel={closeInspector}
          />
        </form>
      </InspectorSurface>
    {/if}
  </div>
</section>

<!-- Dirty guard: one confirmation for close / row-switch / new / cancel. -->
<ConfirmDialog
  open={discardOpen}
  title={t('timers.discardTitle')}
  body={t('timers.discardBody')}
  confirmLabel={t('timers.discard')}
  cancelLabel={t('timers.keepEditing')}
  danger
  onCancel={cancelDiscard}
  onConfirm={confirmDiscard}
/>

<style>
  /* the deck: full-width list, docked inspector only when a row is open. */
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

  /* The editor form fills the inspector surface below its head; the Scroller
     scrolls and the EditorFooter stays pinned at the bottom. */
  .inspector-form { display: flex; flex-direction: column; min-height: 0; flex: 1; }
</style>
