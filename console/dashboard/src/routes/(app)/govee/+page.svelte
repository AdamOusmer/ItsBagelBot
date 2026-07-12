<script lang="ts">
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    Card,
    PageHead,
    Scroller,
    ConfirmDialog,
    InspectorSurface,
    MasterToggle,
    AlertBanner,
    DeckList,
    EmptyState,
    Button,
    ButtonLink,
    toast,
    getI18n,
    type GoveeDevice
  } from '@bagel/shared';
  import GoveeLightRow from '$lib/components/govee/GoveeLightRow.svelte';
  import GoveeRewardEditor from '$lib/components/govee/GoveeRewardEditor.svelte';

  let { data } = $props();
  const { t } = getI18n();

  // Local mirrors, reseeded on each SSR load (the /events stream re-runs the
  // loader after every confirmed write).
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

  type ActionResult = { ok?: boolean; missingScope?: boolean; error?: string };
  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  // formResult is the shared enhance handler for the API-key forms: on success it
  // optionally flips an optimistic mirror, toasts, and reloads.
  function formResult(okMsg: string, failMsg: string, onOk?: () => void): SubmitFunction {
    return () =>
      async ({ result }) => {
        const payload = payloadOf(result);
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

  // --- Inspector -------------------------------------------------------------
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
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok !== false) {
        toast('ok', t('govee.toastSaved'));
        // Save keeps the inspector open on the bound light; invalidateAll
        // reseeds the binding so it reads current.
        await invalidateAll();
        return;
      }
      if (payload?.missingScope) {
        missingScope = true;
        // Keep the inspector open so the draft survives the reconnect prompt.
        return;
      }
      toast('err', payload?.error ?? t('govee.toastSaveFailed'));
    };
  };

  // --- Delete (confirm; Twitch reward deletion is not undoable) ---------------
  let deleteTarget = $state<GoveeDevice | null>(null);
  let deleting = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);

  const deleteSubmit: SubmitFunction = () => {
    deleting = true;
    return async ({ result }) => {
      deleting = false;
      const target = deleteTarget;
      deleteTarget = null;
      const payload = payloadOf(result);
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
      toast('err', payload?.error ?? t('govee.toastDeleteFailed'));
    };
  };

  const colorDevices = (devices: GoveeDevice[]) => devices.filter((d) => d.color);
</script>

<section class="screen active">
  <a class="back" href="/modules"><Icon name="x" size={13} /> {t('govee.back')}</a>
  <PageHead eyebrow={t('govee.eyebrow')} description={t('govee.description')}>
    {t('govee.titlePre')} <em>{t('govee.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('govee.degraded')}</AlertBanner>
  {/if}

  {#if missingScope}
    <!-- Unavailable state explained in TEXT with the required Twitch action. -->
    <AlertBanner variant="warn" icon="power">
      {t('govee.reconnect')}
      {#snippet action()}
        <ButtonLink variant="primary" href="/login?next=/govee" data-sveltekit-reload>{t('govee.reconnectCta')}</ButtonLink>
      {/snippet}
    </AlertBanner>
  {/if}

  <!-- Master switch -->
  <div class="toolbar">
    <MasterToggle
      action="?/toggle"
      bind:enabled
      label={t('govee.masterLabel')}
      hint={enabled ? t('govee.masterHintOn') : t('govee.masterHintOff')}
      ariaLabel={t('govee.masterAria')}
      failMessage={t('govee.masterFail')}
    />
  </div>

  <!-- Step 1 (prerequisite): the API key. The device + reward UI below is gated
       on a key being on file, so setup always comes before management. -->
  <Card>
    <div class="step">
      <span class="step-index" aria-hidden="true">1</span>
      <div class="step-body">
        <h2>{t('govee.keyTitle')}</h2>
        <p class="muted-text">
          {t('govee.keyHelpPre')} <strong>{t('govee.keyPath')}</strong>. {t('govee.keyHelpPost')}
        </p>
        {#if keyPresent}
          <div class="row">
            <span class="ok-pill"><Icon name="check" size={13} /> {t('govee.keyOnFile')}</span>
            <form method="POST" action="?/clearKey" use:enhance={formResult(t('govee.keyRemoved'), t('govee.keyRemoveFailed'), () => (keyPresent = false))}>
              <Button variant="destructive" type="submit">{t('govee.keyRemove')}</Button>
            </form>
          </div>
        {:else}
          <!-- The key never renders back: type="password", autocomplete off, and
               the server stores it encrypted (it is never echoed to the client). -->
          <form method="POST" action="?/saveKey" use:enhance={formResult(t('govee.keySaved'), t('govee.keySaveFailed'), () => (keyPresent = true))} class="row">
            <input class="input" type="password" name="key" placeholder={t('govee.keyPlaceholder')} aria-label={t('govee.keyFieldLabel')} autocomplete="off" required />
            <Button variant="primary" type="submit">{t('govee.keySave')}</Button>
          </form>
        {/if}
      </div>
    </div>
  </Card>

  {#if keyPresent}
    <!-- Step 2: bind a reward to each light. The deck mirrors the commands +
         channel-points decks so the management screens read as one system. Its
         off-state is communicated by the master-switch text above, not by
         dimming the deck. -->
    <div class="deck" class:inspecting={!!selected}>
      <div class="deck-lead">
        <span class="step-index sm" aria-hidden="true">2</span>
        <h2 class="lights-title">{t('govee.lightsTitle')}</h2>
      </div>

      <DeckList>
        {#await data.devices}
          <p class="loading" role="status"><span class="spinner" aria-hidden="true"></span> {t('govee.loadingLights')}</p>
        {:then dr}
          {@const lights = colorDevices(dr.devices ?? [])}
          {#if dr.error}
            <!-- Never surface the raw provider error (it may carry the key);
                 show a safe, actionable localized message instead. -->
            <p class="err-text" role="alert"><Icon name="ban" size={13} /> {t('govee.devicesError')}</p>
          {:else if lights.length === 0}
            <EmptyState icon="power" title={t('govee.noLights')} />
          {:else}
            <div class="list">
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
          <Scroller fill padding="16px" data-lenis-prevent>
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
  .back {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    text-decoration: none;
    margin-bottom: 10px;
  }
  .back:hover { color: var(--bb-white); }
  .back:focus-visible { outline: 2px solid var(--bb-focus, var(--bb-tan)); outline-offset: 2px; border-radius: 4px; }

  .toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 18px; }

  .step { display: flex; gap: 14px; align-items: flex-start; }
  .step-index {
    flex: none;
    width: 34px;
    height: 34px;
    border-radius: 8px;
    display: grid;
    place-items: center;
    background: rgba(201, 168, 124, 0.12);
    border: 1px solid var(--glass-border);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-mono, "DM Mono", monospace);
    font-weight: 600;
    font-size: 14px;
  }
  .step-index.sm { width: 26px; height: 26px; font-size: 12px; border-radius: 6px; }
  .step-body { flex: 1; min-width: 0; }
  .step-body h2 { margin: 0 0 6px; font-family: var(--bb-font-display); font-weight: 700; font-size: 15px; color: var(--bb-white); }
  .muted-text { color: var(--bb-muted); font-family: var(--bb-font-body); font-size: 13px; line-height: 1.55; margin: 0 0 14px; }
  .muted-text strong { color: var(--bb-tan-light); font-weight: 600; }

  .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
  .input {
    padding: 8px 12px;
    border-radius: 6px;
    border: 1px solid var(--rule);
    background: rgba(240, 236, 228, 0.04);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13px;
    min-width: 13rem;
  }
  .input:focus { outline: none; border-color: var(--bb-tan, #c9a87c); }
  .input::placeholder { color: var(--bb-muted); opacity: 0.7; }

  .ok-pill { display: inline-flex; align-items: center; gap: 6px; color: var(--bb-green-glow); font-family: var(--bb-font-body); font-size: 13px; font-weight: 600; }

  /* Deck (list + docked inspector), mirroring the channel-points page. The
     step-2 heading spans both columns as a lead row. */
  .deck {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
    margin-top: 16px;
  }
  .deck-lead { display: flex; align-items: center; gap: 10px; }
  .lights-title { margin: 0; font-family: var(--bb-font-display); font-weight: 700; font-size: 15px; color: var(--bb-white); }
  @media (min-width: 1080px) {
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 440px; }
    .deck.inspecting .deck-lead { grid-column: 1 / -1; }
  }
  .list :global(.row-shell:last-child) { border-bottom: none; }

  .loading, .err-text { display: flex; align-items: center; gap: 10px; padding: 16px; margin: 0; font-family: var(--bb-font-body); font-size: 13px; }
  .loading { color: var(--bb-muted); }
  .err-text { color: #cf8a78; gap: 6px; }
  .spinner {
    width: 14px; height: 14px; border-radius: 50%;
    border: 2px solid var(--rule-strong); border-top-color: var(--bb-tan-light);
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) {
    .spinner { animation: none; }
  }
</style>
