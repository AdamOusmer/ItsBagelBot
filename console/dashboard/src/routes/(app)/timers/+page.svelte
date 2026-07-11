<script lang="ts">
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    PageHead,
    Scroller,
    ConfirmDialog,
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
  import TimerRow from '$lib/components/timers/TimerRow.svelte';
  import TimerEditor from '$lib/components/timers/TimerEditor.svelte';

  let { data } = $props();
  const { t } = getI18n();

  // Local source of truth, reseeded when a fresh SSR load lands (the /events
  // invalidation stream re-runs the loader after every confirmed write).
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

  // --- Inspector -------------------------------------------------------------
  const NEW = '__new__';
  let expanded = $state<string | null>(null);
  let editorDraft = $state<TimerDef | null>(null);
  let busy = $state(false);

  function openNew() {
    editorDraft = blankTimer();
    expanded = NEW;
  }
  function openEdit(tmr: TimerDef) {
    if (expanded === tmr.id) {
      closeEditor();
      return;
    }
    editorDraft = { ...tmr };
    expanded = tmr.id;
  }
  function closeEditor() {
    expanded = null;
    editorDraft = null;
  }

  type ActionResult = { ok?: boolean; error?: string };

  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  function failed(payload: ActionResult | undefined, fallbackKey: string) {
    toast('err', payload?.error ?? t(fallbackKey));
  }

  // --- Save (create or update from the inspector) -----------------------------
  const saveSubmit: SubmitFunction = () => {
    const d = editorDraft;
    if (!d) return;
    const creating = expanded === NEW;
    busy = true;
    return async ({ result }) => {
      busy = false;
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok) {
        toast('ok', t(creating ? 'timers.toastCreated' : 'timers.toastSaved'));
        closeEditor();
        await invalidateAll();
        return;
      }
      failed(payload, 'timers.toastSaveFailed');
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

  // --- Delete (confirm dialog) -------------------------------------------------
  let deleteTarget = $state<TimerDef | null>(null);
  let deleting = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);

  const deleteSubmit: SubmitFunction = () => {
    deleting = true;
    return async ({ result }) => {
      deleting = false;
      const target = deleteTarget;
      deleteTarget = null;
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok) {
        if (target) {
          timers = timers.filter((x) => x.id !== target.id);
          if (expanded === target.id) closeEditor();
          toast('ok', t('timers.toastDeleted'));
        }
        await invalidateAll();
        return;
      }
      failed(payload, 'timers.toastDeleteFailed');
    };
  };

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && editorDraft) closeEditor();
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
      <button class="btn primary" onclick={openNew} disabled={expanded === NEW}>
        <Icon name="plus" size={14} /> {t('timers.newTimer')}
      </button>
    {/snippet}
  </PageToolbar>

  <!-- The deck: ledger list left, docked inspector right — same layout as
       channelpoints/commands, so every management screen reads as one system. -->
  <div class="deck {editorDraft ? 'inspecting' : ''}">
    <DeckList>
      <div class="list">
        {#each rows as tmr, i (tmr.id)}
          <TimerRow
            timer={tmr}
            index={i + 1}
            expanded={expanded === tmr.id}
            onExpand={() => openEdit(tmr)}
            onDelete={() => (deleteTarget = tmr)}
            toggleSubmit={toggleSubmit(tmr)}
          />
        {/each}
        {#if rows.length === 0}
          <EmptyState icon="clock" title={t('timers.emptyTitle')} body={t('timers.emptySub')}>
            <button class="btn primary" onclick={openNew}><Icon name="plus" size={14} /> {t('timers.newTimer')}</button>
          </EmptyState>
        {/if}
      </div>
    </DeckList>

    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="inspector-backdrop"
      class:open={!!editorDraft}
      role="presentation"
      onclick={closeEditor}
      onkeydown={(e) => {
        if (e.key === 'Enter') closeEditor();
      }}
    ></div>
    <aside class="inspector" class:open={!!editorDraft} aria-label={t('timers.inspector')}>
      <div class="inspector-head">
        <span class="inspector-tag">
          {#if editorDraft}
            {expanded === NEW ? t('timers.newTimer') : t('timers.editing')}
          {:else}
            {t('timers.inspector')}
          {/if}
        </span>
        {#if editorDraft}
          <button class="mini" type="button" aria-label={t('common.cancel')} onclick={closeEditor}>
            <Icon name="x" size={14} />
          </button>
        {/if}
      </div>
      {#if editorDraft}
        <Scroller fill padding="16px" data-lenis-prevent>
          {#key expanded}
            <TimerEditor bind:draft={editorDraft} isNew={expanded === NEW} {busy} onCancel={closeEditor} onSubmit={saveSubmit} />
          {/key}
        </Scroller>
      {:else}
        <div class="inspector-idle">
          <span class="idle-glyph"><Icon name="clock" size={18} /></span>
          <p>{t('timers.inspectorIdle')}</p>
          <button class="btn ghost" onclick={openNew}><Icon name="plus" size={13} /> {t('timers.newTimer')}</button>
        </div>
      {/if}
    </aside>
  </div>
</section>

<svelte:window onkeydown={onKey} />

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('timers.deleteTitle')}
  body={t('timers.deleteBody')}
  confirmLabel={t('timers.del')}
  cancelLabel={t('common.cancel')}
  danger
  busy={deleting}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/delete" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="id" value={deleteTarget?.id ?? ''} />
</form>

<style>
  /* ── the deck: list + docked inspector (mirrors channelpoints/commands) ── */
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
    top: 62px;
    border: 1px solid var(--rule);
    border-top-color: var(--rule-strong);
    border-radius: 8px;
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
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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
    border-radius: 8px;
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
      border-radius: 8px 8px 0 0;
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
</style>
