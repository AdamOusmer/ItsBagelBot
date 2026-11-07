<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import { untrack } from 'svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import EditorFooter from '@bagel/ui/svelte/EditorFooter.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Spinner from '@bagel/ui/svelte/Spinner.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { focusFirstInvalid } from '@bagel/ui/svelte/forms';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import {
    MasterToggle,
    getI18n,
    GOVEE_GROUP_MIN_LIGHTS,
    type GoveeDevice,
    type GoveeGroup,
    type GoveeLight,
    actionPayload,
    type ActionOk
  } from '@bagel/kit';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import GoveeLightRow from '#lib/components/govee/GoveeLightRow.svelte';
  import GoveeSetup from '#lib/components/govee/GoveeSetup.svelte';
  import {
    goveeDraftFor,
    goveeErrors,
    goveeFormFields,
    goveeGroupDraftFor,
    goveeGroupErrors,
    goveeGroupFormFields,
    type GoveeDraft
  } from '#lib/components/govee/govee-draft';
  import { warmOnIntent } from '#lib/warm-on-intent';
  const loadGoveeRewardEditor = () => import('#lib/components/govee/GoveeRewardEditor.svelte');

  let { data } = $props();
  const { t } = getI18n();

  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let keyPresent = $state<boolean>(data.keyPresent ?? false);
  // svelte-ignore state_referenced_locally
  let setupOpen = $state(!(data.keyPresent ?? false));
  const showSetup = $derived(!data.degraded && (!keyPresent || setupOpen));
  // svelte-ignore state_referenced_locally
  let bindings = $state([...(data.bindings ?? [])]);
  // svelte-ignore state_referenced_locally
  let groups = $state([...(data.groups ?? [])]);
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      enabled = data.enabled ?? false;
      keyPresent = data.keyPresent ?? false;
      bindings = [...(data.bindings ?? [])];
      groups = [...(data.groups ?? [])];
    }
  });

  const bindingFor = (deviceId: string) => bindings.find((b) => b.device === deviceId) ?? null;

  let missingScope = $state(false);

  type GoveeActionOk = ActionOk & { missingScope?: boolean; code?: string };

  function failed(payload: GoveeActionOk | undefined, fallback: string) {
    if (payload?.missingScope) {
      missingScope = true;
      return;
    }
    if (payload?.code === 'key_invalid') {
      toast('danger', t('govee.keyInvalid'));
      return;
    }
    toast('danger', payload?.error ?? fallback);
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
        failed(payload, t('govee.keySaveFailed'));
        return;
      }
      if (payload?.code === 'key_unchecked') {
        toast('info', t('govee.keyUnchecked'));
      } else {
        toast('success', t('govee.keySaved'));
      }
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
        failed(payload, t('govee.keyRemoveFailed'));
        return;
      }
      keyPresent = false;
      setupOpen = true;
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
  let selectedGroup = $state<{ rewardId: string; options: GoveeLight[] } | null>(null);
  let draft = $state<GoveeDraft | null>(null);
  let busy = $state(false);
  let validationAttempted = $state(false);
  let formEl = $state<HTMLFormElement | null>(null);

  $effect(() => {
    const snap = draft ? { ...draft } : null;
    if (snap) untrack(() => inspector.edit(snap));
  });

  const selectedBinding = $derived(selected ? bindingFor(selected.device) : null);
  const isNew = $derived(selectedGroup ? !selectedGroup.rewardId : !selectedBinding?.rewardId);
  const canSave = $derived(isNew || inspector.dirty);

  function doClose() {
    validationAttempted = false;
    selected = null;
    selectedGroup = null;
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
      selectedGroup = null;
      inspector.open(d.device, base);
      draft = { ...base };
    });
  }
  function closeInspector() {
    discard.guard(doClose);
  }

  function groupOptions(lights: GoveeDevice[], group: GoveeGroup | null): GoveeLight[] {
    const known = lights.map((d) => ({ device: d.device, sku: d.sku, name: d.name }));
    const gone = (group?.lights ?? []).filter((l) => !lights.some((d) => d.device === l.device));
    return [...known, ...gone];
  }

  function openGroup(lights: GoveeDevice[], group: GoveeGroup | null) {
    const rewardId = group?.rewardId ?? '';
    if (selectedGroup?.rewardId === rewardId) {
      closeInspector();
      return;
    }
    discard.guard(() => {
      validationAttempted = false;
      const base = goveeGroupDraftFor(group, () => t('govee.defaultTitle', { name: t('govee.theLights') }));
      selected = null;
      selectedGroup = { rewardId, options: groupOptions(lights, group) };
      inspector.open(rewardId || 'new-group', base);
      draft = { ...base };
    });
  }

  function saveIssues(d: GoveeDraft) {
    return selectedGroup ? { ...goveeErrors(d), ...goveeGroupErrors(d) } : goveeErrors(d);
  }

  function saveFields(d: GoveeDraft) {
    return selectedGroup ? goveeGroupFormFields(d, selectedGroup.rewardId) : goveeFormFields(d);
  }

  const saveSubmit: SubmitFunction = (input) => {
    if (!draft) {
      input.cancel();
      return;
    }
    if (Object.keys(saveIssues(draft)).length) {
      validationAttempted = true;
      input.cancel();
      void focusFirstInvalid(formEl);
      return;
    }
    for (const [name, value] of Object.entries(saveFields(draft))) input.formData.set(name, value);
    const createdGroup = selectedGroup !== null && !selectedGroup.rewardId;
    const requestId = inspector.beginSave()?.requestId;
    busy = true;
    return async ({ result }) => {
      busy = false;
      const payload = actionPayload<GoveeActionOk>(result);
      const ok = isOk(result, payload);
      if (requestId) inspector.resolved(requestId, { type: ok ? 'success' : 'error' });
      if (!ok) {
        failed(payload, t('govee.toastSaveFailed'));
        return;
      }
      if (createdGroup) doClose();
      toast('success', t('govee.toastSaved'));
      await invalidateAll();
    };
  };

  type DeleteTarget = { kind: 'light' | 'group'; id: string; name: string };
  let deleteTarget = $state<DeleteTarget | null>(null);
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
        failed(payload, t('govee.toastDeleteFailed'));
        return;
      }
      if (target && editing(target)) doClose();
      toast('success', t('govee.toastDeleted'));
      await invalidateAll();
    };
  };

  const editing = (target: DeleteTarget) =>
    target.kind === 'group' ? selectedGroup?.rewardId === target.id : selected?.device === target.id;
  const deleteLight = (d: GoveeDevice) => (deleteTarget = { kind: 'light', id: d.device, name: d.name || t('govee.thisLight') });
  const deleteGroup = (rewardId: string, name: string) => (deleteTarget = { kind: 'group', id: rewardId, name });

  const colorDevices = (devices: GoveeDevice[]) => devices.filter((d) => d.color);
</script>

{#snippet devicesError(code?: string)}
  <div class="err-block" role="alert">
    <Text size="sm" tone="danger">{t(code === 'key_rejected' ? 'govee.devicesKeyRejected' : 'govee.devicesError')}</Text>
    <Button variant="secondary" type="button" busy={refreshing} onclick={refreshLights}>{t('govee.devicesRetry')}</Button>
  </div>
{/snippet}

{#if showSetup}
  <GoveeSetup name={data.displayName} devices={data.devices} preview={data.setupPreview === true} onFinish={() => (setupOpen = false)} />
{:else}
<section class="screen active" use:warmOnIntent={[loadGoveeRewardEditor]}>
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
              {@render devicesError(dr.code)}
            {:else if (dr.devices ?? []).length === 0}
              <EmptyState title={t('govee.noDevices')} body={t('govee.noDevicesBody')}>
                <Button variant="secondary" type="button" busy={refreshing} onclick={refreshLights}>{t('govee.noLightsCta')}</Button>
              </EmptyState>
            {:else if lights.length === 0}
              <EmptyState title={t('govee.noLights')} body={t('govee.noLightsBody')}>
                <Button variant="secondary" type="button" busy={refreshing} onclick={refreshLights}>{t('govee.noLightsCta')}</Button>
              </EmptyState>
            {:else}
              <div>
                {#each lights as d (d.device)}
                  <GoveeLightRow
                    name={d.name || d.device}
                    detail={d.sku}
                    binding={bindingFor(d.device)}
                    expanded={selected?.device === d.device}
                    onExpand={() => openLight(d)}
                    onDelete={() => deleteLight(d)}
                  />
                {/each}
              </div>
              <div class="groups">
                <div class="groups-lead">
                  <div class="groups-title">
                    <Heading level={6} as="h2">{t('govee.groupsTitle')}</Heading>
                    <Text size="sm" tone="muted">{t('govee.groupsBody')}</Text>
                  </div>
                  {#if lights.length >= GOVEE_GROUP_MIN_LIGHTS}
                    <Button variant="secondary" type="button" onclick={() => openGroup(lights, null)}>{t('govee.groupNew')}</Button>
                  {/if}
                </div>
                {#each groups as g (g.rewardId)}
                  <GoveeLightRow
                    name={g.name}
                    detail={t('govee.groupLightCount', { n: g.lights.length })}
                    binding={g}
                    expanded={selectedGroup?.rewardId === g.rewardId}
                    onExpand={() => openGroup(lights, g)}
                    onDelete={() => deleteGroup(g.rewardId, g.name)}
                  />
                {/each}
              </div>
            {/if}
          {:catch}
            {@render devicesError()}
          {/await}
        </DeckList>

        {#if (selected || selectedGroup) && draft}
          <InspectorSurface
            open
            title={selectedGroup ? draft.name.trim() || t('govee.groupNew') : selected?.name || t('govee.thisLight')}
            controls="govee-editor"
            closeLabel={t('govee.closeEditor')}
            onClose={closeInspector}
          >
            <form
              method="POST"
              action={selectedGroup ? '?/saveGroup' : '?/saveReward'}
              novalidate
              use:enhance={saveSubmit}
              class="inspector-form"
              bind:this={formEl}
            >
              {#if selected}
                <input type="hidden" name="device" value={selected.device} />
                <input type="hidden" name="sku" value={selected.sku} />
                <input type="hidden" name="deviceName" value={selected.name} />
              {/if}
              <Scroller fill padding="16px" smooth>
                {#key selected?.device ?? `group:${selectedGroup?.rewardId}`}
                  {#await loadGoveeRewardEditor() then { default: GoveeRewardEditor }}
                    <GoveeRewardEditor
                      bind:draft
                      colors={data.colors}
                      groupOptions={selectedGroup?.options ?? null}
                      canDelete={selectedGroup ? !!selectedGroup.rewardId : !!selectedBinding}
                      {busy}
                      attempted={validationAttempted}
                      onRequestDelete={() => {
                        if (selected) deleteLight(selected);
                        else if (selectedGroup) deleteGroup(selectedGroup.rewardId, draft?.name ?? '');
                      }}
                    />
                  {/await}
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
{/if}

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
  body={t(deleteTarget?.kind === 'group' ? 'govee.groupDeleteBody' : 'govee.deleteBody', { name: deleteTarget?.name ?? '' })}
  confirmLabel={t('govee.deleteConfirm')}
  cancelLabel={t('govee.deleteCancel')}
  busy={deleting}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
  tone="danger"
/>
<form method="POST" action={deleteTarget?.kind === 'group' ? '?/deleteGroup' : '?/deleteReward'} use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name={deleteTarget?.kind === 'group' ? 'reward_id' : 'device'} value={deleteTarget?.id ?? ''} />
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

  .groups-lead { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; padding: 16px; }
  .groups-title { display: grid; gap: 4px; min-width: 0; flex: 1; }

  .state { display: flex; align-items: center; gap: 10px; padding: 16px; }
  .err-block { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; padding: 16px; }

  .inspector-form { display: flex; flex-direction: column; min-height: 0; flex: 1; }
</style>
