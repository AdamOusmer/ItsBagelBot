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
    blankReward,
    type ChannelPointReward,
    MasterToggle,
    PageToolbar,
    AlertBanner,
    DeckList,
    EmptyState
  } from '@bagel/shared';
  import RewardRow from '$lib/components/channelpoints/RewardRow.svelte';
  import RewardEditor from '$lib/components/channelpoints/RewardEditor.svelte';

  let { data } = $props();
  const { t } = getI18n();

  // Local source of truth, reseeded when a fresh SSR load lands (the /events
  // invalidation stream re-runs the loader after every confirmed write).
  // svelte-ignore state_referenced_locally
  let rewards = $state<ChannelPointReward[]>(data.rewards ?? []);
  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      rewards = data.rewards ?? [];
      enabled = data.enabled ?? false;
    }
  });

  const rows = $derived(rewards.toSorted((a, b) => a.title.localeCompare(b.title)));

  // Set once a write comes back missing the redemption scope: the grant
  // predates channel:manage:redemptions, so the broadcaster must re-consent.
  let missingScope = $state(false);

  // --- Inspector -------------------------------------------------------------
  const NEW = '__new__';
  let expanded = $state<string | null>(null);
  let editorDraft = $state<ChannelPointReward | null>(null);
  let busy = $state(false);

  function openNew() {
    editorDraft = blankReward();
    expanded = NEW;
  }
  function openEdit(r: ChannelPointReward) {
    if (expanded === r.id) {
      closeEditor();
      return;
    }
    editorDraft = { ...r };
    expanded = r.id;
  }
  function closeEditor() {
    expanded = null;
    editorDraft = null;
  }

  type ActionResult = { ok?: boolean; missingScope?: boolean; error?: string };

  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  function failed(payload: ActionResult | undefined, fallbackKey: string) {
    if (payload?.missingScope) {
      missingScope = true;
      closeEditor();
      return;
    }
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
        toast('ok', t(creating ? 'channelpoints.toastCreated' : 'channelpoints.toastSaved', { name: d.title }));
        closeEditor();
        await invalidateAll();
        return;
      }
      failed(payload, 'channelpoints.toastSaveFailed');
    };
  };

  // --- Row quick toggle (show/hide on Twitch, optimistic) ---------------------
  const toggleSubmit =
    (r: ChannelPointReward): SubmitFunction =>
    () => {
      const was = r.isEnabled;
      rewards = rewards.map((x) => (x.id === r.id ? { ...x, isEnabled: !was } : x));
      return async ({ result }) => {
        const payload = payloadOf(result);
        if (result.type === 'success' && payload?.ok) return;
        rewards = rewards.map((x) => (x.id === r.id ? { ...x, isEnabled: was } : x));
        failed(payload, 'channelpoints.toastToggleFailed');
      };
    };

  // --- Delete (confirm dialog; Twitch deletion is not undoable) ---------------
  let deleteTarget = $state<ChannelPointReward | null>(null);
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
          rewards = rewards.filter((x) => x.id !== target.id);
          if (expanded === target.id) closeEditor();
          toast('ok', t('channelpoints.toastDeleted', { name: target.title }));
        }
        await invalidateAll();
        return;
      }
      failed(payload, 'channelpoints.toastDeleteFailed');
    };
  };

  const selected = $derived(expanded && expanded !== NEW ? rewards.find((r) => r.id === expanded) : undefined);

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && editorDraft) closeEditor();
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('channelpoints.eyebrow')} description={t('channelpoints.description')}>
    {t('channelpoints.titlePre')}<em>{t('channelpoints.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('channelpoints.degraded')}</AlertBanner>
  {/if}

  {#if missingScope}
    <AlertBanner variant="warn" icon="power">
      {t('channelpoints.reconnect')}
      {#snippet action()}
        <a class="btn primary" href="/login?next=/channelpoints" data-sveltekit-reload>{t('channelpoints.reconnectCta')}</a>
      {/snippet}
    </AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      <MasterToggle
        action="?/toggle"
        bind:enabled
        label={t('channelpoints.botOn')}
        hint={t('channelpoints.botOnHint')}
        ariaLabel={t('channelpoints.botOn')}
        failMessage={t('channelpoints.toastToggleFailed')}
      />
    {/snippet}
    {#snippet trail()}
      <button class="btn primary" onclick={openNew} disabled={expanded === NEW}>
        <Icon name="plus" size={14} /> {t('channelpoints.newReward')}
      </button>
    {/snippet}
  </PageToolbar>

  <!-- The deck: ledger list left, docked inspector right — same layout as the
       commands page, so the two management screens read as one system. -->
  <div class="deck {editorDraft ? 'inspecting' : ''}">
    <DeckList>
      <div class="list">
        {#each rows as r, i (r.id)}
          <RewardRow
            reward={r}
            index={i + 1}
            expanded={expanded === r.id}
            onExpand={() => openEdit(r)}
            onDelete={() => (deleteTarget = r)}
            toggleSubmit={toggleSubmit(r)}
          />
        {/each}
        {#if rows.length === 0}
          <EmptyState icon="gem" title={t('channelpoints.emptyTitle')} body={t('channelpoints.emptySub')}>
            <button class="btn primary" onclick={openNew}><Icon name="plus" size={14} /> {t('channelpoints.newReward')}</button>
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
      onkeydown={(e) => { if (e.key === 'Enter') closeEditor(); }}
    ></div>
    <aside class="inspector" class:open={!!editorDraft} aria-label={t('channelpoints.inspector')}>
      <div class="inspector-head">
        <span class="inspector-tag">
          {#if editorDraft}
            {expanded === NEW ? t('channelpoints.newReward') : t('channelpoints.editing', { name: selected?.title ?? '' })}
          {:else}
            {t('channelpoints.inspector')}
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
            <RewardEditor bind:draft={editorDraft} isNew={expanded === NEW} {busy} onCancel={closeEditor} onSubmit={saveSubmit} />
          {/key}
        </Scroller>
      {:else}
        <div class="inspector-idle">
          <span class="idle-glyph"><Icon name="gem" size={18} /></span>
          <p>{t('channelpoints.inspectorIdle')}</p>
          <button class="btn ghost" onclick={openNew}><Icon name="plus" size={13} /> {t('channelpoints.newReward')}</button>
        </div>
      {/if}
    </aside>
  </div>
</section>

<svelte:window onkeydown={onKey} />

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('channelpoints.deleteTitle')}
  body={t('channelpoints.deleteBody', { name: deleteTarget?.title ?? '' })}
  confirmLabel={t('channelpoints.del')}
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
  /* ── the deck: list + docked inspector (mirrors the commands page) ── */
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
