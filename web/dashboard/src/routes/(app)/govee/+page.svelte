<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Card,
    PageHead,
    PageToolbar,
    Scroller,
    ConfirmDialog,
    InspectorSurface,
    MasterToggle,
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
    TextLink,
    toast,
    getI18n,
    type GoveeDevice,
    actionPayload,
    toastFailure,
    type ActionOk,
    Tag,
  } from '@bagel/kit';
  import GoveeLightRow from '$lib/components/govee/GoveeLightRow.svelte';
  import GoveeRewardEditor from '$lib/components/govee/GoveeRewardEditor.svelte';

  let { data } = $props();
  const { t } = getI18n();
  const failed = toastFailure(toast, t);

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

  type GoveeActionOk = ActionOk & { missingScope?: boolean };

  function formResult(okMsg: string, failMsg: string, onOk?: () => void): SubmitFunction {
    return () =>
      async ({ result }) => {
        const payload = actionPayload<GoveeActionOk>(result);
        if (result.type === 'success' && payload?.ok !== false) {
          onOk?.();
          toast('ok', okMsg);
          await invalidateAll();
          return;
        }
        if (payload?.missingScope) {
          missingScope = true;
          return;
        }
        toast('err', payload?.error ?? failMsg);
      };
  }

  let selected = $state<GoveeDevice | null>(null);
  let busy = $state(false);

  function openLight(d: GoveeDevice) {
    selected = selected?.device === d.device ? null : d;
  }
  function closeInspector() {
    selected = null;
  }

  const saveSubmit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const payload = actionPayload<GoveeActionOk>(result);
      if (result.type === 'success' && payload?.ok !== false) {
        toast('ok', t('govee.toastSaved'));
        await invalidateAll();
        return;
      }
      if (payload?.missingScope) {
        missingScope = true;
        return;
      }
      failed(payload, 'govee.toastSaveFailed');
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
      if (result.type === 'success' && payload?.ok !== false) {
        if (target && selected?.device === target.device) closeInspector();
        toast('ok', t('govee.toastDeleted'));
        await invalidateAll();
        return;
      }
      if (payload?.missingScope) {
        missingScope = true;
        return;
      }
      failed(payload, 'govee.toastDeleteFailed');
    };
  };

  const colorDevices = (devices: GoveeDevice[]) => devices.filter((d) => d.color);
</script>

<section class="screen active">
  <div class="back"><TextLink variant="quiet" icon="arrowLeft" href="/modules" label={t('govee.back')} /></div>
  <PageHead eyebrow={t('govee.eyebrow')} description={t('govee.description')}>
    {t('govee.titlePre')} <em>{t('govee.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('govee.degraded')}</AlertBanner>
  {/if}

  {#if missingScope}
    <AlertBanner variant="warn">
      {t('govee.reconnect')}
      {#snippet action()}
        <ButtonLink variant="primary" href="/login?next=/govee" data-sveltekit-reload>{t('govee.reconnectCta')}</ButtonLink>
      {/snippet}
    </AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
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
    <div class="step">
      <span class="step-index" aria-hidden="true">1</span>
      <div class="step-body">
        <Heading level={6} as="h2">{t('govee.keyTitle')}</Heading>
        <Text size="sm" tone="muted">
          {t('govee.keyHelpPre')} <strong class="key-path">{t('govee.keyPath')}</strong>. {t('govee.keyHelpPost')}
        </Text>
        {#if keyPresent}
          <div class="row">
            <Tag tone="live" mark="solid">{t('govee.keyOnFile')}</Tag>
            <form method="POST" action="?/clearKey" use:enhance={formResult(t('govee.keyRemoved'), t('govee.keyRemoveFailed'), () => (keyPresent = false))}>
              <Button variant="destructive" type="submit">{t('govee.keyRemove')}</Button>
            </form>
          </div>
        {:else}
          <form method="POST" action="?/saveKey" use:enhance={formResult(t('govee.keySaved'), t('govee.keySaveFailed'), () => (keyPresent = true))} class="row">
            <Input type="password" name="key" placeholder={t('govee.keyPlaceholder')} aria-label={t('govee.keyFieldLabel')} autocomplete="off" required />
            <Button variant="primary" type="submit">{t('govee.keySave')}</Button>
          </form>
        {/if}
      </div>
    </div>
  </Card>

  {#if keyPresent}
    <div class="lights">
      <DeckLayout inspecting={!!selected} width="440px">
        <div class="deck-lead">
          <span class="step-index sm" aria-hidden="true">2</span>
          <Heading level={6} as="h2">{t('govee.lightsTitle')}</Heading>
        </div>

        <DeckList>
          {#await data.devices}
            <div class="state" role="status"><Spinner /><Text as="span" size="sm" tone="muted">{t('govee.loadingLights')}</Text></div>
          {:then dr}
            {@const lights = colorDevices(dr.devices ?? [])}
            {#if dr.error}
              <!-- Never surface the raw provider error: it may carry the key. -->
              <div class="state"><Text size="sm" tone="danger" role="alert">{t('govee.devicesError')}</Text></div>
            {:else if lights.length === 0}
              <EmptyState title={t('govee.noLights')} />
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
          {/await}
        </DeckList>

        {#if selected}
          <InspectorSurface
            open
            title={selected.name || t('govee.thisLight')}
            controls="govee-editor"
            closeLabel={t('govee.closeEditor')}
            onClose={closeInspector}
          >
            <Scroller fill padding="16px" smooth>
              {#key selected.device}
                <GoveeRewardEditor
                  device={selected}
                  binding={bindingFor(selected.device)}
                  colors={data.colors}
                  {busy}
                  onSubmit={saveSubmit}
                  onCancel={closeInspector}
                  onRequestDelete={() => (deleteTarget = selected)}
                />
              {/key}
            </Scroller>
          </InspectorSurface>
        {/if}
      </DeckLayout>
    </div>
  {/if}
</section>

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('govee.deleteTitle')}
  body={t('govee.deleteBody', { name: deleteTarget?.name || t('govee.thisLight') })}
  confirmLabel={t('govee.deleteConfirm')}
  cancelLabel={t('govee.deleteCancel')}
  danger
  busy={deleting}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/deleteReward" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="device" value={deleteTarget?.device ?? ''} />
</form>

<style>
  .back { margin-bottom: 10px; }

  .step { display: flex; gap: 14px; align-items: flex-start; }
  .step-index {
    flex: none;
    width: 34px;
    height: 34px;
    border-radius: var(--bb-radius-sm);
    display: grid;
    place-items: center;
    background: rgba(var(--bb-tan-rgb), 0.12);
    border: 1px solid var(--bb-glass-border);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-mono);
    font-weight: 600;
    font-size: var(--bb-text-sm);
  }
  .step-index.sm { width: 26px; height: 26px; font-size: var(--bb-text-xs); border-radius: var(--bb-radius-xs); }
  .step-body { flex: 1; min-width: 0; display: grid; gap: 6px; }
  .key-path { color: var(--bb-tan-light); font-weight: 600; }

  .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; margin-top: 8px; }

  .lights { margin-top: 16px; }
  .deck-lead { grid-column: 1 / -1; display: flex; align-items: center; gap: 10px; }

  .state { display: flex; align-items: center; gap: 10px; padding: 16px; }
</style>
