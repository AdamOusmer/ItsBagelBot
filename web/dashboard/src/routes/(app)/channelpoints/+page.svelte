<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import { untrack } from 'svelte';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    PageHead,
    Scroller,
    ConfirmDialog,
    EditorFooter,
    InspectorSurface,
    Button,
    ButtonLink,
    toast,
    getI18n,
    blankReward,
    type ChannelPointReward,
    MasterToggle,
    PageToolbar,
    AlertBanner,
    DeckList,
    EmptyState,
    actionPayload,
    focusFirstInvalid,
    createDiscardGuard,
    type ActionOk,
  } from '@bagel/kit';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import RewardRow from '$lib/components/channelpoints/RewardRow.svelte';
  import RewardEditor from '$lib/components/channelpoints/RewardEditor.svelte';
  import { namespaceRewardMessage, rewardDraftFrom, rewardErrors } from '$lib/components/channelpoints/reward-draft';

  let { data } = $props();
  const { t } = getI18n();

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

  let missingScope = $state(false);

  const NEW = '__new__';
  const inspector = createInspector<ChannelPointReward>();
  let draft = $state<ChannelPointReward | null>(null);
  let counterOn = $state(false);
  let busy = $state(false);
  let validationAttempted = $state(false);
  let formEl = $state<HTMLFormElement | null>(null);

  $effect(() => {
    const snap = draft ? { ...draft } : null;
    if (snap) untrack(() => inspector.edit(snap));
  });

  const creating = $derived(inspector.selectedId === NEW);
  const canSave = $derived(creating || inspector.dirty);

  function doClose() {
    validationAttempted = false;
    inspector.reset();
    draft = null;
  }

  const discard = createDiscardGuard(() => inspector.dirty, doClose);

  function openDraft(id: string, reward: ChannelPointReward) {
    validationAttempted = false;
    const base = rewardDraftFrom(reward);
    counterOn = !!base.counter.trim();
    inspector.open(id, base);
    draft = { ...base };
  }
  function openNew() {
    discard.guard(() => openDraft(NEW, blankReward()));
  }
  function openEdit(r: ChannelPointReward) {
    if (inspector.selectedId === r.id) {
      closeEditor();
      return;
    }
    discard.guard(() => openDraft(r.id, r));
  }
  function closeEditor() {
    discard.guard(doClose);
  }

  type RewardActionOk = ActionOk & { missingScope?: boolean; duplicateTitle?: boolean };

  function failed(payload: RewardActionOk | undefined, fallbackKey: string) {
    if (payload?.missingScope) {
      missingScope = true;
      doClose();
      return;
    }
    if (payload?.duplicateTitle) {
      toast('err', t('channelpoints.toastDuplicateTitle'));
      return;
    }
    toast('err', payload?.error ?? t(fallbackKey));
  }

  const saveSubmit: SubmitFunction = (input) => {
    if (!draft) {
      input.cancel();
      return;
    }
    if (Object.keys(rewardErrors(draft, counterOn)).length) {
      validationAttempted = true;
      input.cancel();
      void focusFirstInvalid(formEl);
      return;
    }
    const message = namespaceRewardMessage(draft.message);
    const payload = { ...draft, message };
    input.formData.set('reward', JSON.stringify(payload));
    input.formData.set('counter_enabled', counterOn ? 'true' : 'false');
    const started = inspector.beginSave();
    const requestId = started?.requestId;
    const wasCreating = creating;
    busy = true;
    return async ({ result }) => {
      busy = false;
      const reply = actionPayload<RewardActionOk>(result);
      const ok = result.type === 'success' && reply?.ok === true;
      const applied = requestId ? inspector.resolved(requestId, { type: ok ? 'success' : 'error' }) : false;
      if (!ok) {
        failed(reply, 'channelpoints.toastSaveFailed');
        return;
      }
      toast('ok', t(wasCreating ? 'channelpoints.toastCreated' : 'channelpoints.toastSaved', { name: payload.title }));
      if (wasCreating && applied) doClose();
      await invalidateAll();
    };
  };

  const toggleSubmit =
    (r: ChannelPointReward): SubmitFunction =>
    () => {
      const was = r.isEnabled;
      rewards = rewards.map((x) => (x.id === r.id ? { ...x, isEnabled: !was } : x));
      return async ({ result }) => {
        const payload = actionPayload<RewardActionOk>(result);
        if (result.type === 'success' && payload?.ok) return;
        rewards = rewards.map((x) => (x.id === r.id ? { ...x, isEnabled: was } : x));
        failed(payload, 'channelpoints.toastToggleFailed');
      };
    };

  let deleteTarget = $state<ChannelPointReward | null>(null);
  let deleting = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);

  const deleteSubmit: SubmitFunction = () => {
    deleting = true;
    return async ({ result }) => {
      deleting = false;
      const target = deleteTarget;
      deleteTarget = null;
      const payload = actionPayload<RewardActionOk>(result);
      if (result.type === 'success' && payload?.ok) {
        if (target) {
          rewards = rewards.filter((x) => x.id !== target.id);
          if (inspector.selectedId === target.id) doClose();
          toast('ok', t('channelpoints.toastDeleted', { name: target.title }));
        }
        await invalidateAll();
        return;
      }
      failed(payload, 'channelpoints.toastDeleteFailed');
    };
  };

  const selected = $derived(
    inspector.selectedId && !creating ? rewards.find((r) => r.id === inspector.selectedId) : undefined
  );
</script>

<section class="screen active">
  <PageHead eyebrow={t('channelpoints.eyebrow')} description={t('channelpoints.description')}>
    {t('channelpoints.titlePre')}<em>{t('channelpoints.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('channelpoints.degraded')}</AlertBanner>
  {/if}

  {#if missingScope}
    <AlertBanner variant="warn">
      {t('channelpoints.reconnect')}
      {#snippet action()}
        <ButtonLink variant="primary" href="/login?next=/channelpoints" data-sveltekit-reload>{t('channelpoints.reconnectCta')}</ButtonLink>
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
      <Button variant="primary" onclick={openNew} disabled={creating}>
        {t('channelpoints.newReward')}
      </Button>
    {/snippet}
  </PageToolbar>

  <div class="deck {inspector.isOpen ? 'inspecting' : ''}">
    <DeckList>
      {#if rows.length}
        <ul class="bb-list reward-list" aria-label={t('channelpoints.listLabel')}>
          {#each rows as r, i (r.id)}
            <RewardRow
              reward={r}
              index={i + 1}
              expanded={inspector.selectedId === r.id}
              onExpand={() => openEdit(r)}
              onDelete={() => (deleteTarget = r)}
              toggleSubmit={toggleSubmit(r)}
            />
          {/each}
        </ul>
      {:else}
        <EmptyState title={t('channelpoints.emptyTitle')} body={t('channelpoints.emptySub')}>
          <Button variant="primary" onclick={openNew}>{t('channelpoints.newReward')}</Button>
        </EmptyState>
      {/if}
    </DeckList>

    {#if inspector.isOpen && draft}
      <InspectorSurface
        open
        title={creating ? t('channelpoints.newReward') : t('channelpoints.editing', { name: selected?.title ?? '' })}
        controls="reward-editor"
        closeLabel={t('common.cancel')}
        onClose={closeEditor}
      >
        <form
          method="POST"
          action={creating ? '?/create' : '?/update'}
          novalidate
          use:enhance={saveSubmit}
          class="inspector-form"
          bind:this={formEl}
        >
          <Scroller fill padding="16px" smooth>
            {#key inspector.selectedId}
              <RewardEditor bind:draft bind:counterOn attempted={validationAttempted} />
            {/key}
          </Scroller>
          <EditorFooter
            status={inspector.status}
            dirty={inspector.dirty}
            canSave={canSave && !busy}
            saveLabel={creating ? t('channelpoints.create') : t('channelpoints.saveChanges')}
            cancelLabel={t('common.cancel')}
            savingLabel={t('channelpoints.saving')}
            savedLabel={t('channelpoints.saved')}
            errorLabel={t('channelpoints.toastSaveFailed')}
            dirtyLabel={t('channelpoints.unsavedChanges')}
            onCancel={closeEditor}
          />
        </form>
      </InspectorSurface>
    {/if}
  </div>
</section>

<ConfirmDialog
  open={discard.open}
  title={t('channelpoints.discardTitle')}
  body={t('channelpoints.discardBody')}
  confirmLabel={t('channelpoints.discard')}
  cancelLabel={t('channelpoints.keepEditing')}
  danger
  onCancel={discard.cancel}
  onConfirm={discard.confirm}
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
  .deck {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  @media (min-width: 1080px) {
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 420px; }
  }

  .reward-list :global(li:last-child .row-shell) { border-bottom: none; }

  .inspector-form { display: flex; flex-direction: column; min-height: 0; flex: 1; }
</style>
