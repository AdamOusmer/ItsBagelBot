<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    Card,
    PageHead,
    PageToolbar,
    ConfirmDialog,
    MasterToggle,
    AlertBanner,
    EmptyState,
    Button,
    ButtonLink,
    Field,
    SettingRow,
    CommandList,
    toast,
    getI18n,
    SPOTIFY_SR_PERMS,
    type SpotifySrConfig,
    type SpotifyRedeemConfig,
    type SpotifySrPerm
  } from '@bagel/shared';
  import SpotifyRewardEditor from '$lib/components/spotify/SpotifyRewardEditor.svelte';

  let { data } = $props();
  const { t } = getI18n();

  // Local mirrors, reseeded on each SSR load (the /events stream re-runs the
  // loader after every confirmed write).
  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let connected = $state<boolean>(data.connected ?? false);
  // svelte-ignore state_referenced_locally
  let sr = $state<SpotifySrConfig>(data.sr ?? { enabled: false, perm: 'everyone', allowOffline: false });

  // liveOnly is the inverse of the stored allowOffline flag, on by default —
  // the same shape govee's lights use, so the two switches read identically.
  // svelte-ignore state_referenced_locally
  let liveOnly = $state(!(data.sr?.allowOffline ?? false));

  // The chat commands this module answers. Sourced from the module's own
  // registration rather than written from memory: the page previously
  // documented only the bare add, so every other spelling was undiscoverable.
  const SR_COMMANDS: { cmd: string; key: string; mod?: boolean }[] = [
    { cmd: '!sr <song>', key: 'spotify.cmdAdd' },
    { cmd: '!song', key: 'spotify.cmdView' },
    { cmd: '!sr remove', key: 'spotify.cmdRetract' },
    { cmd: '!skip', key: 'spotify.cmdSkip', mod: true },
    { cmd: '!sr remove <n>', key: 'spotify.cmdRemoveAt', mod: true },
    { cmd: '!sr clear', key: 'spotify.cmdClear', mod: true }
  ];
  // Resolved through the current locale on every render, same as the rest of
  // the page's copy, so a language switch updates the descriptions too.
  const srCommands = $derived(SR_COMMANDS.map((row) => ({ cmd: row.cmd, desc: t(row.key), mod: row.mod })));
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
      sr = data.sr ?? { enabled: false, perm: 'everyone', allowOffline: false };
      liveOnly = !(data.sr?.allowOffline ?? false);
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
      // Revert-on-failure: the optimistic mirror above may be wrong now.
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

  // --- Reward editor -----------------------------------------------------------
  // The editor shows when there is nothing bound yet (create) or when the
  // broadcaster asked to edit the bound one.
  let editing = $state(false);
  const showEditor = $derived(!redeem.rewardId || editing);
  let busy = $state(false);

  const saveSubmit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok !== false) {
        editing = false;
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
        editing = false;
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
    <!-- Unavailable state explained in TEXT with the required Twitch action. -->
    <AlertBanner variant="warn" icon="music">
      {t('spotify.reconnect')}
      {#snippet action()}
        <ButtonLink variant="primary" href="/login?next=/songqueue" data-sveltekit-reload>{t('spotify.reconnectCta')}</ButtonLink>
      {/snippet}
    </AlertBanner>
  {/if}

  <!-- Step 1 (prerequisite): connect the account. The request-path setup below
       works either way, but requests cannot queue without it. -->
  <Card>
    <div class="step">
      <span class="step-index" aria-hidden="true">1</span>
      <div class="step-body">
        <h2>{t('spotify.connectTitle')}</h2>
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
      </div>
    </div>
  </Card>

  <!-- Master switch -->
  <PageToolbar>
    {#snippet lead()}
      <MasterToggle
        action="?/toggle"
        bind:enabled
        label={t('spotify.masterLabel')}
        hint={enabled ? t('spotify.masterHintOn') : t('spotify.masterHintOff')}
        ariaLabel={t('spotify.masterAria')}
        failMessage={t('spotify.masterFail')}
      />
    {/snippet}
  </PageToolbar>

  <!-- Path A: chat command -->
  <Card>
    <div class="path-head">
      <span class="step-index sm" aria-hidden="true">A</span>
      <div class="path-title">
        <h2>{t('spotify.srTitle')}</h2>
        <p class="muted-text">{t('spotify.srHelp')}</p>
      </div>
    </div>
    <form method="POST" action="?/sr" use:enhance={srSubmit} bind:this={srForm}>
      <SettingRow
        label={t('spotify.srEnableLabel')}
        description={sr.enabled ? t('spotify.srEnableOn') : t('spotify.srEnableOff')}
        bind:checked={sr.enabled}
        onchange={srChanged}
        name="sr_enabled"
      />

      <!-- Same control govee's lights use, down to the inverted flag and the
           warn styling when the gate is lifted: on means requests only queue
           while you are live, which is the default. -->
      <SettingRow
        label={t('spotify.liveOnlyLabel')}
        description={liveOnly ? t('spotify.liveOnlyOn') : t('spotify.liveOnlyOff')}
        warn={!liveOnly}
        bind:checked={liveOnly}
        onchange={srChanged}
        name="allow_offline"
        onValue=""
        offValue="on"
      />

      <Field label={t('spotify.srPermLabel')}>
        <select class="input" name="perm" value={sr.perm} onchange={srChanged}>
          {#each SPOTIFY_SR_PERMS as p (p)}
            <option value={p}>{t(PERM_LABEL_KEYS[p])}</option>
          {/each}
        </select>
      </Field>

      <!-- The page documented only the bare add. Every other spelling existed
           in sesame and nowhere in the UI, so nobody could discover them. -->
      <CommandList title={t('spotify.cmdsTitle')} modLabel={t('spotify.cmdModOnly')} commands={srCommands} />
    </form>
  </Card>

  <!-- Path B: channel points -->
  <Card>
    <div class="path-head">
      <span class="step-index sm" aria-hidden="true">B</span>
      <div class="path-title">
        <h2>{t('spotify.redeemTitle')}</h2>
        <p class="muted-text">{t('spotify.redeemHelp')}</p>
      </div>
    </div>

    <form method="POST" action="?/redeemToggle" use:enhance={redeemToggleSubmit} bind:this={redeemForm}>
      <SettingRow
        label={t('spotify.redeemEnableLabel')}
        description={redeem.enabled ? t('spotify.redeemEnableOn') : t('spotify.redeemEnableOff')}
        bind:checked={redeem.enabled}
        onchange={redeemToggled}
        name="redeem_enabled"
      />
    </form>

    {#if showEditor}
      <!-- Keyed on the binding identity + mode so switching create↔edit reseeds
           the editor's local draft state (the govee inspector's {#key} rule). -->
      {#key (redeem.rewardId || 'new') + String(editing)}
        <SpotifyRewardEditor
          redeem={redeem}
          {busy}
          onSubmit={saveSubmit}
          onCancel={() => (editing = false)}
          onRequestDelete={() => (deletePending = true)}
        />
      {/key}
    {:else if redeem.reward}
      <div class="bound-row">
        <span class="bound-title"><Icon name="gem" size={13} /> {redeem.reward.title}</span>
        <span class="muted-text">{t('spotify.costPts', { n: redeem.reward.cost })}</span>
        <Button variant="secondary" icon="edit" onclick={() => (editing = true)} disabled={busy}>{t('spotify.editReward')}</Button>
      </div>
    {:else}
      <EmptyState icon="gem" title={t('spotify.noRewardTitle')} body={t('spotify.noRewardBody')} />
    {/if}
  </Card>

  <p class="both-note muted-text">{t('spotify.bothNote')}</p>
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
  .muted-text { color: var(--bb-muted); font-family: var(--bb-font-body); font-size: 13px; line-height: 1.55; margin: 0; }
  .step-body .muted-text { margin-bottom: 14px; }
  .step-body .muted-text:only-child { margin-bottom: 0; }

  .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }

  .ok-pill { display: inline-flex; align-items: center; gap: 6px; color: var(--bb-green-glow); font-family: var(--bb-font-body); font-size: 13px; font-weight: 600; }

  /* Request-path sections share the step header shape so A/B read as one system */
  .path-head { display: flex; gap: 10px; align-items: flex-start; margin-bottom: 14px; }
  .path-title h2 { margin: 0 0 4px; font-family: var(--bb-font-display); font-weight: 700; font-size: 15px; color: var(--bb-white); }
  .path-title .muted-text { font-size: 12.5px; }

  .bound-row { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; padding: 11px 12px; border: 1px solid var(--rule); border-radius: 8px; }
  .bound-title { display: inline-flex; align-items: center; gap: 7px; font-family: var(--bb-font-display); font-weight: 700; font-size: 13.5px; color: var(--bb-white); flex: 1; min-width: 0; }

  /* On narrow screens the title would otherwise be squeezed into a sliver
     between the cost and the button, wrapping one word per line. Stack the
     three pieces instead. */
  @media (max-width: 520px) {
    .bound-row { flex-direction: column; align-items: flex-start; gap: 8px; }
    .bound-title { flex: none; }
  }

  .both-note { display: flex; align-items: center; gap: 6px; margin-top: 14px; font-size: 12px; }

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
</style>
