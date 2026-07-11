<script lang="ts">
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    PageHead,
    Scroller,
    ConfirmDialog,
    InspectorSurface,
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

  // Dirty guard: close / row-switch / new confirm before dropping an in-progress
  // reward edit.
  const committed = $derived.by<ChannelPointReward | null>(() => {
    if (!editorDraft) return null;
    if (expanded === NEW) return blankReward();
    return rewards.find((r) => r.id === expanded) ?? null;
  });
  const isDirty = $derived(
    !!editorDraft && committed !== null ? JSON.stringify(editorDraft) !== JSON.stringify(committed) : false
  );

  let discardOpen = $state(false);
  let afterDiscard: (() => void) | null = null;
  function guarded(action: () => void) {
    if (isDirty) {
      afterDiscard = action;
      discardOpen = true;
    } else {
      action();
    }
  }
  function confirmDiscard() {
    discardOpen = false;
    const a = afterDiscard;
    afterDiscard = null;
    a?.();
  }
  function cancelDiscard() {
    discardOpen = false;
    afterDiscard = null;
  }
  // Unguarded close (after a save / delete, or a scope error).
  function doClose() {
    expanded = null;
    editorDraft = null;
  }

  function openNew() {
    guarded(() => {
      editorDraft = blankReward();
      expanded = NEW;
    });
  }
  function openEdit(r: ChannelPointReward) {
    if (expanded === r.id) {
      closeEditor();
      return;
    }
    guarded(() => {
      editorDraft = { ...r };
      expanded = r.id;
    });
  }
  function closeEditor() {
    guarded(doClose);
  }

  type ActionResult = { ok?: boolean; missingScope?: boolean; error?: string };

  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  function failed(payload: ActionResult | undefined, fallbackKey: string) {
    if (payload?.missingScope) {
      missingScope = true;
      doClose();
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
        // A create closes (no client id to keep editing); an update keeps the
        // inspector open. invalidateAll reseeds rewards so the draft reads clean.
        if (creating) doClose();
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
          if (expanded === target.id) doClose();
          toast('ok', t('channelpoints.toastDeleted', { name: target.title }));
        }
        await invalidateAll();
        return;
      }
      failed(payload, 'channelpoints.toastDeleteFailed');
    };
  };

  const selected = $derived(expanded && expanded !== NEW ? rewards.find((r) => r.id === expanded) : undefined);
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

    {#if editorDraft}
      <InspectorSurface
        open
        title={expanded === NEW ? t('channelpoints.newReward') : t('channelpoints.editing', { name: selected?.title ?? '' })}
        controls="reward-editor"
        closeLabel={t('common.cancel')}
        onClose={closeEditor}
      >
        <Scroller fill padding="16px" data-lenis-prevent>
          {#key expanded}
            <RewardEditor bind:draft={editorDraft} isNew={expanded === NEW} {busy} onCancel={closeEditor} onSubmit={saveSubmit} />
          {/key}
        </Scroller>
      </InspectorSurface>
    {/if}
  </div>
</section>

<ConfirmDialog
  open={discardOpen}
  title={t('channelpoints.discardTitle')}
  body={t('channelpoints.discardBody')}
  confirmLabel={t('channelpoints.discard')}
  cancelLabel={t('channelpoints.keepEditing')}
  danger
  onCancel={cancelDiscard}
  onConfirm={confirmDiscard}
/>

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
  /* the deck: full-width list until a selection opens the docked inspector. */
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
</style>
