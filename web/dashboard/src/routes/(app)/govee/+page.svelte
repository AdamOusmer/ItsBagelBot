<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import { untrack } from 'svelte';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Card,
    PageHead,
    PageToolbar,
    Scroller,
    ConfirmDialog,
    EditorFooter,
    InspectorSurface,
    AlertBanner,
    DeckLayout,
    DeckList,
    EmptyState,
    Button,
    ButtonLink,
    Heading,
    Input,
    Spinner,
    Text,
    toast,
    focusFirstInvalid,
    createDiscardGuard,
    Tag
  } from '@bagel/ui/svelte';
  import {
    MasterToggle,
    getI18n,
    type GoveeDevice,
    actionPayload,
    type ActionOk
  } from '@bagel/kit';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import GoveeLightRow from '$lib/components/govee/GoveeLightRow.svelte';
  import GoveeRewardEditor from '$lib/components/govee/GoveeRewardEditor.svelte';
  import { goveeDraftFor, goveeErrors, goveeFormFields, type GoveeDraft } from '$lib/components/govee/govee-draft';

  let { data } = $props();
  const { t } = getI18n();

  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let keyPresent = $state<boolean>(data.keyPresent ?? false);
  // svelte-ignore state_referenced_locally
  let bindings = $state([...(data.bindings ?? [])]);
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      enabled = data.enabled ?? false;
      keyPresent = data.keyPresent ?? false;
      bindings = [...(data.bindings ?? [])];
    }
  });

  const bindingFor = (deviceId: string) => bindings.find((b) => b.device === deviceId) ?? null;

  let missingScope = $state(false);

  type GoveeActionOk = ActionOk & { missingScope?: boolean; code?: string };

  function failed(payload: GoveeActionOk | undefined, fallbackKey: string) {
    if (payload?.missingScope) {
      missingScope = true;
      return;
    }
    if (payload?.code === 'key_invalid') {
      toast('danger', t('govee.keyInvalid'));
      return;
    }
    toast('danger', payload?.error ?? t(fallbackKey));
  }

  const isOk = (result: { type: string }, payload: GoveeActionOk | undefined) =>
    result.type === 'success' && payload?.ok !== false;

  let keySaving = $state(false);
  const saveKeySubmit: SubmitFunction = () => {
    keySaving = true;
    return async ({ result }) => {
      keySaving = false;
      const payload = actionPayload<GoveeActionOk>(result);
      if (!isOk(result, payload)) {
        failed(payload, 'govee.keySaveFailed');
        return;
      }
      toast('success', t('govee.keySaved'));
      await invalidateAll();
    };
  };

  let keyRemovePending = $state(false);
  let keyRemoving = $state(false);
  let keyRemoveForm = $state<HTMLFormElement | null>(null);
  const clearKeySubmit: SubmitFunction = () => {
    keyRemoving = true;
    return async ({ result }) => {
      keyRemoving = false;
      keyRemovePending = false;
      const payload = actionPayload<GoveeActionOk>(result);
      if (!isOk(result, payload)) {
        failed(payload, 'govee.keyRemoveFailed');
        return;
      }
      keyPresent = false;
      doClose();
      toast('success', t('govee.keyRemoved'));
      await invalidateAll();
    };
  };

  let refreshing = $state(false);
  async function refreshLights() {
    refreshing = true;
    try {
      await invalidateAll();
    } finally {
      refreshing = false;
    }
  }

  const inspector = createInspector<GoveeDraft>();
  let selected = $state<GoveeDevice | null>(null);
  let draft = $state<GoveeDraft | null>(null);
  let busy = $state(false);
  let validationAttempted = $state(false);
  let formEl = $state<HTMLFormElement | null>(null);

  $effect(() => {
    const snap = draft ? { ...draft } : null;
    if (snap) untrack(() => inspector.edit(snap));
  });

  const selectedBinding = $derived(selected ? bindingFor(selected.device) : null);
  const isNew = $derived(!selectedBinding?.rewardId);
  const canSave = $derived(isNew || inspector.dirty);

  function doClose() {
    validationAttempted = false;
    selected = null;
    inspector.reset();
    draft = null;
  }
  const discard = createDiscardGuard(() => inspector.dirty, doClose);

  function openLight(d: GoveeDevice) {
    if (selected?.device === d.device) {
      closeInspector();
      return;
    }
    discard.guard(() => {
      validationAttempted = false;
      const base = goveeDraftFor(d, bindingFor(d.device), (name) =>
        t('govee.defaultTitle', { name: name || t('govee.theLights') })
      );
      selected = d;
      inspector.open(d.device, base);
      draft = { ...base };
    });
  }
  function closeInspector() {
    discard.guard(doClose);
  }

  const saveSubmit: SubmitFunction = (input) => {
    if (!draft) {
      input.cancel();
      return;
    }
    if (Object.keys(goveeErrors(draft)).length) {
      validationAttempted = true;
      input.cancel();
      void focusFirstInvalid(formEl);
      return;
    }
    for (const [name, value] of Object.entries(goveeFormFields(draft))) input.formData.set(name, value);
    const requestId = inspector.beginSave()?.requestId;
    busy = true;
    return async ({ result }) => {
      busy = false;
      const payload = actionPayload<GoveeActionOk>(result);
      const ok = isOk(result, payload);
      if (requestId) inspector.resolved(requestId, { type: ok ? 'success' : 'error' });
      if (!ok) {
        failed(payload, 'govee.toastSaveFailed');
        return;
      }
      toast('success', t('govee.toastSaved'));
      await invalidateAll();
    };
  };

  let deleteTarget = $state<GoveeDevice | null>(null);
  let deleting = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);

  const deleteSubmit: SubmitFunction = () => {
    deleting = true;
    return async ({ result }) => {
      deleting = false;
      const target = deleteTarget;
      deleteTarget = null;
      const payload = actionPayload<GoveeActionOk>(result);
      if (!isOk(result, payload)) {
        failed(payload, 'govee.toastDeleteFailed');
        return;
      }
      if (target && selected?.device === target.device) doClose();
      toast('success', t('govee.toastDeleted'));
      await invalidateAll();
    };
  };

  const colorDevices = (devices: GoveeDevice[]) => devices.filter((d) => d.color);
</script>

{#snippet devicesError()}
  <div class="err-block" role="alert">
    <Text size="sm" tone="danger">{t('govee.devicesError')}</Text>
    <Button variant="secondary" type="button" busy={refreshing} onclick={refreshLights}>{t('govee.devicesRetry')}</Button>
  </div>
{/snippet}

<section class="screen active">
  <PageHead eyebrow={t('govee.eyebrow')} description={t('govee.description')}>
    {t('govee.titlePre')} <em>{t('govee.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('govee.degraded')}</AlertBanner>
  {/if}

  {#if missingScope}
    <AlertBanner tone="warning">
      {t('govee.reconnect')}
      {#snippet actions()}
        <ButtonLink variant="primary" href="/login?next=/govee" data-sveltekit-reload>{t('govee.reconnectCta')}</ButtonLink>
      {/snippet}
    </AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet leading()}
      <MasterToggle
        action="?/toggle"
        bind:enabled
        label={t('govee.masterLabel')}
        hint={enabled ? t('govee.masterHintOn') : t('govee.masterHintOff')}
        ariaLabel={t('govee.masterAria')}
        failMessage={t('govee.masterFail')}
      />
    {/snippet}
  </PageToolbar>

  <Card>
    <div class="key-card">
      <Heading level={6} as="h2">{keyPresent ? t('govee.keyCardTitle') : t('govee.keyTitle')}</Heading>
      {#if keyPresent}
        <div class="row">
          <Tag tone="live" mark="solid">{t('govee.keyOnFile')}</Tag>
          <Button type="button" onclick={() => (keyRemovePending = true)} tone="danger">{t('govee.keyRemove')}</Button>
        </div>
      {:else}
        <Text size="sm" tone="muted">
          {t('govee.keyHelpPre')} <strong class="key-path">{t('govee.keyPath')}</strong>. {t('govee.keyHelpPost')}
        </Text>
        <form method="POST" action="?/saveKey" use:enhance={saveKeySubmit} class="row key-form">
          <span class="key-input">
            <Input fill type="password" name="key" placeholder={t('govee.keyPlaceholder')} aria-label={t('govee.keyFieldLabel')} autocomplete="off" required />
          </span>
          <Button variant="primary" type="submit" busy={keySaving}>{t('govee.keySave')}</Button>
        </form>
      {/if}
    </div>
  </Card>

  {#if keyPresent}
    <div class="lights">
      <DeckLayout inspecting={inspector.isOpen} width="440px">
        <div class="deck-lead">
          <Heading level={6} as="h2">{t('govee.lightsTitle')}</Heading>
        </div>

        <DeckList>
          {#await data.devices}
            <div class="state" role="status"><Spinner /><Text as="span" size="sm" tone="muted">{t('govee.loadingLights')}</Text></div>
          {:then dr}
            {@const lights = colorDevices(dr.devices ?? [])}
            {#if dr.error}
              {@render devicesError()}
            {:else if lights.length === 0}
              <EmptyState title={t('govee.noLights')} body={t('govee.noLightsBody')}>
                <Button variant="secondary" type="button" busy={refreshing} onclick={refreshLights}>{t('govee.noLightsCta')}</Button>
              </EmptyState>
            {:else}
              <div>
                {#each lights as d (d.device)}
                  <GoveeLightRow
                    device={d}
                    binding={bindingFor(d.device)}
                    expanded={selected?.device === d.device}
                    onExpand={() => openLight(d)}
                    onDelete={() => (deleteTarget = d)}
                  />
                {/each}
              </div>
            {/if}
          {:catch}
            {@render devicesError()}
          {/await}
        </DeckList>

        {#if selected && draft}
          <InspectorSurface
            open
            title={selected.name || t('govee.thisLight')}
            controls="govee-editor"
            closeLabel={t('govee.closeEditor')}
            onClose={closeInspector}
          >
            <form
              method="POST"
              action="?/saveReward"
              novalidate
              use:enhance={saveSubmit}
              class="inspector-form"
              bind:this={formEl}
            >
              <input type="hidden" name="device" value={selected.device} />
              <input type="hidden" name="sku" value={selected.sku} />
              <input type="hidden" name="deviceName" value={selected.name} />
              <Scroller fill padding="16px" smooth>
                {#key selected.device}
                  <GoveeRewardEditor
                    bind:draft
                    colors={data.colors}
                    canDelete={!!selectedBinding}
                    {busy}
                    attempted={validationAttempted}
                    onRequestDelete={() => (deleteTarget = selected)}
                  />
                {/key}
              </Scroller>
              <EditorFooter
                status={inspector.status}
                dirty={inspector.dirty}
                canSave={canSave && !busy}
                saveLabel={isNew ? t('govee.create') : t('govee.saveChanges')}
                cancelLabel={t('common.cancel')}
                savingLabel={t('govee.saving')}
                savedLabel={t('govee.saved')}
                errorLabel={t('govee.toastSaveFailed')}
                dirtyLabel={t('govee.unsavedChanges')}
                onCancel={closeInspector}
              />
            </form>
          </InspectorSurface>
        {/if}
      </DeckLayout>
    </div>
  {/if}
</section>

<ConfirmDialog
  open={discard.open}
  title={t('govee.discardTitle')}
  body={t('govee.discardBody')}
  confirmLabel={t('govee.discard')}
  cancelLabel={t('govee.keepEditing')}
  onCancel={discard.cancel}
  onConfirm={discard.confirm}
  tone="danger"
/>

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('govee.deleteTitle')}
  body={t('govee.deleteBody', { name: deleteTarget?.name || t('govee.thisLight') })}
  confirmLabel={t('govee.deleteConfirm')}
  cancelLabel={t('govee.deleteCancel')}
  busy={deleting}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
  tone="danger"
/>
<form method="POST" action="?/deleteReward" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="device" value={deleteTarget?.device ?? ''} />
</form>

<ConfirmDialog
  open={keyRemovePending}
  title={t('govee.keyRemoveTitle')}
  body={t('govee.keyRemoveBody')}
  confirmLabel={t('govee.keyRemove')}
  cancelLabel={t('govee.deleteCancel')}
  busy={keyRemoving}
  onCancel={() => (keyRemovePending = false)}
  onConfirm={() => keyRemoveForm?.requestSubmit()}
  tone="danger"
/>
<form method="POST" action="?/clearKey" use:enhance={clearKeySubmit} bind:this={keyRemoveForm} hidden></form>

<style>
  .key-card { display: grid; gap: 6px; }
  .key-path { color: var(--bb-tan-light); font-weight: 600; }

  .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
  .key-form { margin-top: 8px; }
  .key-input { min-width: 13rem; flex: 1; max-width: 26rem; }

  .lights { margin-top: 16px; }
  .deck-lead { grid-column: 1 / -1; display: flex; align-items: center; gap: 10px; }

  .state { display: flex; align-items: center; gap: 10px; padding: 16px; }
  .err-block { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; padding: 16px; }

  .inspector-form { display: flex; flex-direction: column; min-height: 0; flex: 1; }
</style>
