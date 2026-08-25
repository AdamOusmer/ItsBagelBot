<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import { onMount } from 'svelte';
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
    Button,
    ButtonLink,
    Field,
    Switch,
    toast,
    getI18n,
    moduleDef,
    SPOTIFY_SR_PERMS,
    type SpotifySrConfig,
    type SpotifyRedeemConfig,
    type SpotifySrPerm
  } from '@bagel/shared';
  import SpotifyRewardEditor from '$lib/components/spotify/SpotifyRewardEditor.svelte';
  import SpotifyRewardRow from '$lib/components/spotify/SpotifyRewardRow.svelte';
  import ModuleCommandList from '$lib/components/modules/ModuleCommandList.svelte';

  let { data } = $props();
  const { t } = getI18n();

  // Chat-commands reference from the shared catalog so this page never drifts
  // from the generic /modules/[id] ledger (the quotes pattern).
  const songCommands = moduleDef('songqueue')?.commands ?? [];

  // Local mirrors, reseeded on each SSR load (the /events stream re-runs the
  // loader after every confirmed write).
  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let connected = $state<boolean>(data.connected ?? false);
  // svelte-ignore state_referenced_locally
  let sr = $state<SpotifySrConfig>(data.sr ?? { enabled: false, perm: 'everyone' });
  // svelte-ignore state_referenced_locally
  let redeem = $state<SpotifyRedeemConfig>(
    data.redeem ?? { enabled: false, rewardId: '', onRedeem: 'fulfill', replyMessage: '', reward: null }
  );
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      enabled = data.enabled ?? false;
      connected = data.connected ?? false;
      sr = data.sr ?? { enabled: false, perm: 'everyone' };
      redeem = data.redeem ?? { enabled: false, rewardId: '', onRedeem: 'fulfill', replyMessage: '', reward: null };
    }
  });

  const PERM_LABEL_KEYS: Record<SpotifySrPerm, string> = {
    everyone: 'spotify.permEveryone',
    sub: 'spotify.permSub',
    vip: 'spotify.permVip',
    mod: 'spotify.permMod',
    broadcaster: 'spotify.permBroadcaster'
  };
  const ERROR_SLUG_KEYS: Record<string, string> = {
    state: 'spotify.errState',
    oauth: 'spotify.errOauth',
    notoken: 'spotify.errNoToken',
    unconfigured: 'spotify.errUnconfigured',
    store: 'spotify.errStore'
  };

  onMount(() => {
    if (data.justConnected) toast('ok', t('spotify.connectedToast'));
  });

  type ActionResult = { ok?: boolean; missingScope?: boolean; error?: string };
  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  // formResult is the shared enhance handler for simple forms: on success it
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

  let missingScope = $state(false);

  // --- Command path (!sr): switch + permission tier post together on change ---
  const srSubmit: SubmitFunction = () =>
    async ({ result }) => {
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok !== false) {
        toast('ok', t('spotify.srSaved'));
        await invalidateAll();
        return;
      }
      if (payload?.missingScope) {
        missingScope = true;
        return;
      }
      toast('err', payload?.error ?? t('spotify.srSaveFailed'));
      await invalidateAll();
    };

  const redeemToggleSubmit: SubmitFunction = () =>
    async ({ result }) => {
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok !== false) {
        await invalidateAll();
        return;
      }
      if (payload?.missingScope) {
        missingScope = true;
        return;
      }
      toast('err', payload?.error ?? t('spotify.masterFail'));
      await invalidateAll();
    };

  // --- Inspector (govee deck: one row, docked editor) -----------------------
  let inspecting = $state(false);
  let busy = $state(false);

  function openReward() {
    inspecting = !inspecting;
  }
  function closeInspector() {
    inspecting = false;
  }

  const saveSubmit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok !== false) {
        toast('ok', t('spotify.toastSaved'));
        await invalidateAll();
        return;
      }
      if (payload?.missingScope) {
        missingScope = true;
        return;
      }
      toast('err', payload?.error ?? t('spotify.toastSaveFailed'));
    };
  };

  // --- Delete (confirm; Twitch reward deletion is not undoable) ---------------
  let deletePending = $state(false);
  let deleting = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);

  const deleteSubmit: SubmitFunction = () => {
    deleting = true;
    return async ({ result }) => {
      deleting = false;
      deletePending = false;
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok !== false) {
        closeInspector();
        toast('ok', t('spotify.toastDeleted'));
        await invalidateAll();
        return;
      }
      if (payload?.missingScope) {
        missingScope = true;
        return;
      }
      toast('err', payload?.error ?? t('spotify.toastDeleteFailed'));
    };
  };

  function srChanged() {
    srForm?.requestSubmit();
  }
  function redeemToggled() {
    redeemForm?.requestSubmit();
  }
  let srForm = $state<HTMLFormElement | null>(null);
  let redeemForm = $state<HTMLFormElement | null>(null);
</script>

<section class="screen active">
  <a class="back" href="/modules"><Icon name="x" size={13} /> {t('spotify.back')}</a>
  <PageHead eyebrow={t('spotify.eyebrow')} description={t('spotify.description')}>
    {t('spotify.titlePre')} <em>{t('spotify.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('spotify.degraded')}</AlertBanner>
  {/if}

  {#if data.errorSlug && ERROR_SLUG_KEYS[data.errorSlug]}
    <AlertBanner variant="warn" icon="ban">{t(ERROR_SLUG_KEYS[data.errorSlug])}</AlertBanner>
  {/if}

  {#if missingScope}
    <AlertBanner variant="warn" icon="music">
      {t('spotify.reconnect')}
      {#snippet action()}
        <ButtonLink variant="primary" href="/login?next=/songqueue" data-sveltekit-reload>{t('spotify.reconnectCta')}</ButtonLink>
      {/snippet}
    </AlertBanner>
  {/if}

  <!-- Master switch first, matching govee: the credential step follows. -->
  <div class="toolbar">
    <MasterToggle
      action="?/toggle"
      bind:enabled
      label={t('spotify.masterLabel')}
      hint={enabled ? t('spotify.masterHintOn') : t('spotify.masterHintOff')}
      ariaLabel={t('spotify.masterAria')}
      failMessage={t('spotify.masterFail')}
    />
  </div>

  <!-- Connect is the only prerequisite. Chat and channel-points are sibling
       paths (either can be on while the other is off), so they share a two-
       column grid instead of a numbered 2-then-3 wizard that implied sequence. -->
  <div class="setup">
    <Card>
      <h2 class="path-title">{t('spotify.connectTitle')}</h2>
      <p class="muted-text">{connected ? t('spotify.connectedHelp') : t('spotify.connectHelp')}</p>
      {#if connected}
        <div class="row">
          <span class="ok-pill"><Icon name="check" size={13} /> {t('spotify.connectedPill')}</span>
          <form method="POST" action="?/disconnect" use:enhance={formResult(t('spotify.disconnectedToast'), t('spotify.disconnectFailed'), () => (connected = false))}>
            <Button variant="destructive" type="submit">{t('spotify.disconnect')}</Button>
          </form>
        </div>
      {:else}
        <ButtonLink variant="primary" icon="link" href="/spotify/connect" data-sveltekit-reload>{t('spotify.connectCta')}</ButtonLink>
      {/if}
    </Card>

    {#if connected}
      <div class="paths" class:inspecting>
        <Card>
          <h2 class="path-title">{t('spotify.srTitle')}</h2>
          <p class="muted-text">{t('spotify.srHelp')}</p>
          <form method="POST" action="?/sr" use:enhance={srSubmit} bind:this={srForm}>
            <div class="enable-row">
              <div class="enable-text">
                <span class="enable-label">{t('spotify.srEnableLabel')}</span>
                <span class="muted-text" id="spotify-sr-desc">{sr.enabled ? t('spotify.srEnableOn') : t('spotify.srEnableOff')}</span>
              </div>
              <Switch bind:checked={sr.enabled} onchange={srChanged} label={t('spotify.srEnableLabel')} describedby="spotify-sr-desc" />
            </div>
            <input type="hidden" name="sr_enabled" value={sr.enabled ? 'on' : ''} />
            {#if sr.enabled}
              <Field label={t('spotify.srPermLabel')}>
                <select class="input" name="perm" value={sr.perm} onchange={srChanged}>
                  {#each SPOTIFY_SR_PERMS as p (p)}
                    <option value={p}>{t(PERM_LABEL_KEYS[p])}</option>
                  {/each}
                </select>
              </Field>
            {:else}
              <input type="hidden" name="perm" value={sr.perm} />
            {/if}
          </form>
        </Card>

        <div class="redeem-col">
          <Card>
            <h2 class="path-title">{t('spotify.redeemTitle')}</h2>
            <p class="muted-text">{t('spotify.redeemHelp')}</p>
            <form method="POST" action="?/redeemToggle" use:enhance={redeemToggleSubmit} bind:this={redeemForm}>
              <div class="enable-row">
                <div class="enable-text">
                  <span class="enable-label">{t('spotify.redeemEnableLabel')}</span>
                  <span class="muted-text" id="spotify-redeem-desc">{redeem.enabled ? t('spotify.redeemEnableOn') : t('spotify.redeemEnableOff')}</span>
                </div>
                <Switch bind:checked={redeem.enabled} onchange={redeemToggled} label={t('spotify.redeemEnableLabel')} describedby="spotify-redeem-desc" />
              </div>
              <input type="hidden" name="redeem_enabled" value={redeem.enabled ? 'on' : ''} />
            </form>
            <div class="reward-slot">
              <SpotifyRewardRow
                {redeem}
                expanded={inspecting}
                onExpand={openReward}
                onDelete={() => (deletePending = true)}
              />
            </div>
          </Card>

          {#if inspecting}
            <InspectorSurface
              open
              title={redeem.reward?.title || t('spotify.thisReward')}
              controls="spotify-editor"
              closeLabel={t('spotify.closeEditor')}
              onClose={closeInspector}
            >
              <Scroller fill padding="16px" data-lenis-prevent>
                {#key (redeem.rewardId || 'new')}
                  <SpotifyRewardEditor
                    {redeem}
                    {busy}
                    onSubmit={saveSubmit}
                    onCancel={closeInspector}
                    onRequestDelete={() => (deletePending = true)}
                  />
                {/key}
              </Scroller>
            </InspectorSurface>
          {/if}
        </div>
      </div>
    {/if}
  </div>

  {#if songCommands.length}
    <div class="cmd-block">
      <DeckList>
        <ModuleCommandList commands={songCommands} headingId="spotify-cmds-h" />
      </DeckList>
    </div>
  {/if}
</section>

<ConfirmDialog
  open={deletePending}
  title={t('spotify.deleteTitle')}
  body={t('spotify.deleteBody')}
  confirmLabel={t('spotify.deleteConfirm')}
  cancelLabel={t('spotify.deleteCancel')}
  danger
  busy={deleting}
  onCancel={() => (deletePending = false)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/deleteReward" use:enhance={deleteSubmit} bind:this={deleteForm} hidden></form>

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

  /* Gap between the connect card and the two request-path cards; `.screen` is
     `display: block` so sibling Cards otherwise sit flush. */
  .setup { display: grid; gap: 16px; }

  .path-title { margin: 0 0 6px; font-family: var(--bb-font-display); font-weight: 700; font-size: 15px; color: var(--bb-white); }
  .muted-text { color: var(--bb-muted); font-family: var(--bb-font-body); font-size: 13px; line-height: 1.55; margin: 0 0 14px; }
  .enable-text .muted-text { margin: 0; font-size: 12px; }

  .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }

  .ok-pill { display: inline-flex; align-items: center; gap: 6px; color: var(--bb-green-glow); font-family: var(--bb-font-body); font-size: 13px; font-weight: 600; }

  .enable-row {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 14px;
  }
  .enable-text { display: grid; gap: 2px; flex: 1; min-width: 0; }
  .enable-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 13px; color: var(--bb-white); }

  .input {
    padding: 8px 12px;
    border-radius: 6px;
    border: 1px solid var(--rule);
    background: rgba(240, 236, 228, 0.04);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13px;
    width: 100%;
    box-sizing: border-box;
  }
  .input:focus { outline: none; border-color: var(--bb-tan, #c9a87c); }

  .paths {
    display: grid;
    grid-template-columns: 1fr;
    gap: 16px;
    align-items: start;
  }
  .redeem-col { display: grid; gap: 16px; min-width: 0; }
  .reward-slot {
    margin: 0 -4px;
    border-top: 1px solid var(--rule);
    padding-top: 4px;
  }
  @media (min-width: 1080px) {
    .paths { grid-template-columns: 1fr 1fr; }
    /* Inspector docks beside the points card, full width under chat — same
       list+pane shape as govee, without squeezing the chat card into a third. */
    .paths.inspecting { grid-template-columns: 1fr; }
    .paths.inspecting .redeem-col { grid-template-columns: minmax(0, 1fr) 440px; }
  }

  .cmd-block { margin-top: 32px; }
</style>
