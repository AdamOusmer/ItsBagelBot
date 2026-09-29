<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import { onMount, untrack } from 'svelte';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Card,
    PageHead,
    PageToolbar,
    Scroller,
    ConfirmDialog,
    EditorFooter,
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
    type SpotifySrPerm,
    blankSpotifySr,
    blankSpotifyRedeem,
    blankSpotifyQuotas,
    SPOTIFY_QUOTA_TIERS,
    type SpotifyQuotas,
    actionPayload,
    focusFirstInvalid,
    createDiscardGuard,
    toastFailure,
    type ActionOk,
  } from '@bagel/kit';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import type { QueueView } from '$lib/server/songqueue-view';
  import SpotifySetup from '$lib/components/spotify/SpotifySetup.svelte';
  import SpotifyConnectionCard from '$lib/components/spotify/SpotifyConnectionCard.svelte';
  import SpotifyQueueCard from '$lib/components/spotify/SpotifyQueueCard.svelte';
  import SpotifyRewardEditor from '$lib/components/spotify/SpotifyRewardEditor.svelte';
  import SpotifyRewardRow from '$lib/components/spotify/SpotifyRewardRow.svelte';
  import ModuleCommandList from '$lib/components/modules/ModuleCommandList.svelte';
  import {
    spotifyDraftFor,
    spotifyErrors,
    spotifyFormFields,
    type SpotifyRewardDraft
  } from '$lib/components/spotify/spotify-draft';

  const QUEUE_POLL_MS = 15000;

  let { data } = $props();
  const { t } = getI18n();
  const failed = toastFailure(toast, t);

  const songCommands = moduleDef('songqueue')?.commands ?? [];

  type QuotaDraft = Record<(typeof SPOTIFY_QUOTA_TIERS)[number], string>;
  const quotaStrings = (q: SpotifyQuotas): QuotaDraft =>
    Object.fromEntries(SPOTIFY_QUOTA_TIERS.map((tier) => [tier, String(q[tier] ?? '')])) as QuotaDraft;

  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let connected = $state<boolean>(data.connected ?? false);
  // svelte-ignore state_referenced_locally
  let scopeGap = $state<string[]>(data.scopeGap ?? []);
  // svelte-ignore state_referenced_locally
  let grantRevoked = $state<boolean>(data.needsReconnect ?? false);
  // svelte-ignore state_referenced_locally
  let sr = $state<SpotifySrConfig>(data.sr ?? blankSpotifySr());
  // svelte-ignore state_referenced_locally
  let redeem = $state<SpotifyRedeemConfig>(data.redeem ?? blankSpotifyRedeem());
  // svelte-ignore state_referenced_locally
  let quotas = $state<SpotifyQuotas>(data.quotas ?? blankSpotifyQuotas());
  // svelte-ignore state_referenced_locally
  let quotaDraft = $state<QuotaDraft>(quotaStrings(data.quotas ?? blankSpotifyQuotas()));
  // svelte-ignore state_referenced_locally
  let app = $state<{ present: boolean; clientId: string }>(data.app ?? { present: false, clientId: '' });
  // svelte-ignore state_referenced_locally
  let queue = $state<QueueView | null>(data.queue ?? null);
  // svelte-ignore state_referenced_locally
  let queueAt = $state(Date.now());
  // svelte-ignore state_referenced_locally
  let seed = data;

  const quotaDirty = $derived(
    SPOTIFY_QUOTA_TIERS.some((tier) => String(quotaDraft[tier] ?? '') !== String(quotas[tier] ?? ''))
  );

  $effect(() => {
    if (data === seed) return;
    seed = data;
    enabled = data.enabled ?? false;
    connected = data.connected ?? false;
    scopeGap = data.scopeGap ?? [];
    grantRevoked = data.needsReconnect ?? false;
    app = data.app ?? { present: false, clientId: '' };
    sr = data.sr ?? blankSpotifySr();
    redeem = data.redeem ?? blankSpotifyRedeem();
    quotas = data.quotas ?? blankSpotifyQuotas();
    if (!untrack(() => quotaDirty)) quotaDraft = quotaStrings(quotas);
    queue = data.queue ?? null;
    queueAt = Date.now();
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
    noapp: 'spotify.errNoApp',
    unconfigured: 'spotify.errUnconfigured',
    store: 'spotify.errStore'
  };
  const QUOTA_LABEL_KEYS: Record<(typeof SPOTIFY_QUOTA_TIERS)[number], string> = {
    everyone: 'spotify.quotaEveryone',
    sub: 'spotify.quotaSub',
    vip: 'spotify.quotaVip',
    mod: 'spotify.quotaMod'
  };

  type SongQueueActionOk = ActionOk & { missingScope?: boolean };
  let missingScope = $state(false);

  const isOk = (result: { type: string }, payload: SongQueueActionOk | undefined) =>
    result.type === 'success' && payload?.ok !== false;

  function reportFailure(payload: SongQueueActionOk | undefined, fallbackKey: string) {
    if (payload?.missingScope) {
      missingScope = true;
      toast('err', t('spotify.reconnect'));
      return;
    }
    failed(payload, fallbackKey);
  }

  let queueBusy = false;
  let queueRefreshing = $state(false);
  async function refreshQueue(manual: boolean) {
    if (queueBusy) return;
    queueBusy = true;
    queueRefreshing = manual;
    try {
      const res = await fetch('/songqueue/queue', { headers: { accept: 'application/json' } });
      if (!res.ok) throw new Error(String(res.status));
      const body = (await res.json()) as { queue: QueueView | null };
      if (body.queue) {
        queue = body.queue;
        queueAt = Date.now();
      }
    } catch {
      if (manual) toast('err', t('spotify.degraded'));
    } finally {
      queueBusy = false;
      queueRefreshing = false;
    }
  }

  const SKIP_ERROR_KEYS: Record<string, string> = {
    no_device: 'spotify.skipNoDevice',
    premium: 'spotify.skipPremium',
    reauth: 'spotify.skipReauth'
  };
  const SKIP_REFRESH_MS = 1200;

  let skipping = $state(false);
  let skipForm = $state<HTMLFormElement | null>(null);

  const skipSubmit: SubmitFunction = ({ cancel }) => {
    if (skipping) {
      cancel();
      return;
    }
    skipping = true;
    return async ({ result }) => {
      skipping = false;
      const payload = actionPayload<SongQueueActionOk & { code?: string }>(result);
      if (isOk(result, payload)) {
        toast('ok', t('spotify.skipped'));
        setTimeout(() => void refreshQueue(false), SKIP_REFRESH_MS);
        return;
      }
      const key = payload?.code ? SKIP_ERROR_KEYS[payload.code] : undefined;
      if (key) toast('err', t(key));
      else reportFailure(payload, 'spotify.skipFailed');
    };
  };

  onMount(() => {
    const poll = setInterval(() => {
      if (!document.hidden) void refreshQueue(false);
    }, QUEUE_POLL_MS);
    if (data.justConnected) toast('ok', t('spotify.connectedToast'));
    return () => clearInterval(poll);
  });

  let connectionAction = $state<'disconnect' | 'clearApp' | null>(null);
  let connectionBusy = $state(false);
  let connectionForm = $state<HTMLFormElement | null>(null);

  const connectionSubmit: SubmitFunction = () => {
    if (!connectionAction || connectionBusy) return;
    const action = connectionAction;
    connectionBusy = true;
    return async ({ result }) => {
      connectionBusy = false;
      connectionAction = null;
      const payload = actionPayload<SongQueueActionOk>(result);
      if (!isOk(result, payload)) {
        reportFailure(payload, action === 'clearApp' ? 'spotify.appRemoveFailed' : 'spotify.disconnectFailed');
        return;
      }
      if (action === 'clearApp') app = { present: false, clientId: '' };
      connected = false;
      toast('ok', t(action === 'clearApp' ? 'spotify.appRemoved' : 'spotify.disconnectedToast'));
      await invalidateAll();
    };
  };

  function settingsSubmit(opts: {
    fields: () => Record<string, string>;
    failKey: string;
    okKey?: string;
    revert: () => void;
  }): SubmitFunction {
    return ({ formData }) => {
      for (const [name, value] of Object.entries(opts.fields())) formData.set(name, value);
      return async ({ result }) => {
        const payload = actionPayload<SongQueueActionOk>(result);
        if (!isOk(result, payload)) {
          opts.revert();
          reportFailure(payload, opts.failKey);
          return;
        }
        if (opts.okKey) toast('ok', t(opts.okKey));
        await invalidateAll();
      };
    };
  }

  const srSubmit = settingsSubmit({
    fields: () => ({
      sr_enabled: sr.enabled ? 'on' : '',
      sr_allow_offline: sr.allowOffline ? 'on' : '',
      perm: sr.perm
    }),
    failKey: 'spotify.srSaveFailed',
    okKey: 'spotify.srSaved',
    revert: () => (sr = data.sr ?? blankSpotifySr())
  });

  const redeemToggleSubmit = settingsSubmit({
    fields: () => ({
      redeem_enabled: redeem.enabled ? 'on' : '',
      redeem_allow_offline: redeem.allowOffline ? 'on' : ''
    }),
    failKey: 'spotify.masterFail',
    revert: () => (redeem = data.redeem ?? blankSpotifyRedeem())
  });

  let quotaSaving = $state(false);
  const quotasSubmit: SubmitFunction = () => {
    quotaSaving = true;
    return async ({ result }) => {
      quotaSaving = false;
      const payload = actionPayload<SongQueueActionOk>(result);
      if (!isOk(result, payload)) {
        reportFailure(payload, 'spotify.quotaSaveFailed');
        return;
      }
      toast('ok', t('spotify.quotaSaved'));
      await invalidateAll();
    };
  };

  let srForm = $state<HTMLFormElement | null>(null);
  let redeemForm = $state<HTMLFormElement | null>(null);
  const srChanged = () => srForm?.requestSubmit();
  const redeemToggled = () => redeemForm?.requestSubmit();
  function enableSr() {
    sr.enabled = true;
    srChanged();
  }

  const inspector = createInspector<SpotifyRewardDraft>();
  let draft = $state<SpotifyRewardDraft | null>(null);
  let busy = $state(false);
  let validationAttempted = $state(false);
  let formEl = $state<HTMLFormElement | null>(null);

  $effect(() => {
    const snap = draft ? { ...draft } : null;
    if (snap) untrack(() => inspector.edit(snap));
  });

  const isNew = $derived(!redeem.rewardId);
  const canSave = $derived(isNew || inspector.dirty);

  function doClose() {
    validationAttempted = false;
    inspector.reset();
    draft = null;
  }
  const discard = createDiscardGuard(() => inspector.dirty, doClose);

  function openReward() {
    if (inspector.isOpen) {
      discard.guard(doClose);
      return;
    }
    validationAttempted = false;
    const base = spotifyDraftFor(redeem, t('spotify.defaultTitle'));
    inspector.open('reward', base);
    draft = { ...base };
  }
  const closeInspector = () => discard.guard(doClose);

  const saveSubmit: SubmitFunction = (input) => {
    if (!draft) {
      input.cancel();
      return;
    }
    if (Object.keys(spotifyErrors(draft)).length) {
      validationAttempted = true;
      input.cancel();
      void focusFirstInvalid(formEl);
      return;
    }
    for (const [name, value] of Object.entries(spotifyFormFields(draft))) input.formData.set(name, value);
    const requestId = inspector.beginSave()?.requestId;
    busy = true;
    return async ({ result }) => {
      busy = false;
      const payload = actionPayload<SongQueueActionOk>(result);
      const ok = isOk(result, payload);
      if (requestId) inspector.resolved(requestId, { type: ok ? 'success' : 'error' });
      if (!ok) {
        reportFailure(payload, 'spotify.toastSaveFailed');
        return;
      }
      toast('ok', t('spotify.toastSaved'));
      await invalidateAll();
    };
  };

  let deletePending = $state(false);
  let deleting = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);

  const deleteSubmit: SubmitFunction = () => {
    deleting = true;
    return async ({ result }) => {
      deleting = false;
      deletePending = false;
      const payload = actionPayload<SongQueueActionOk>(result);
      if (!isOk(result, payload)) {
        reportFailure(payload, 'spotify.toastDeleteFailed');
        return;
      }
      doClose();
      toast('ok', t('spotify.toastDeleted'));
      await invalidateAll();
    };
  };
</script>

{#snippet head()}
  <PageHead eyebrow={t('spotify.eyebrow')} description={t('spotify.description')}>
    {t('spotify.titlePre')} <em>{t('spotify.titleEm')}</em>
  </PageHead>
{/snippet}

{#snippet enableRow(opts: { label: string; desc: string; descId: string; checked: boolean; disabled?: boolean; onchange: (on: boolean) => void })}
  <div class="enable-row">
    <div class="enable-text">
      <span class="enable-label">{opts.label}</span>
      <span class="muted-text" id={opts.descId}>{opts.desc}</span>
    </div>
    <Switch checked={opts.checked} disabled={opts.disabled} onchange={opts.onchange} label={opts.label} describedby={opts.descId} />
  </div>
{/snippet}

{#if data.degraded}
<section class="screen active">
  {@render head()}
  <AlertBanner>{t('spotify.degraded')}</AlertBanner>
</section>
{:else if !connected}
  <SpotifySetup
    name={data.displayName}
    app={app}
    redirectUri={data.redirectUri ?? ''}
    preview={'setupPreview' in data && data.setupPreview === true}
    error={data.errorSlug ? t(ERROR_SLUG_KEYS[data.errorSlug] ?? 'spotify.errOauth') : ''}
    onRemoveApp={() => (connectionAction = 'clearApp')}
  />
{:else}
<section class="screen active">
  {@render head()}

  {#if data.errorSlug && ERROR_SLUG_KEYS[data.errorSlug]}
    <AlertBanner variant="warn">{t(ERROR_SLUG_KEYS[data.errorSlug])}</AlertBanner>
  {/if}

  {#if missingScope}
    <AlertBanner variant="warn">
      {t('spotify.reconnect')}
      {#snippet action()}
        <ButtonLink variant="primary" href="/login?next=/songqueue" data-sveltekit-reload>{t('spotify.reconnectCta')}</ButtonLink>
      {/snippet}
    </AlertBanner>
  {/if}

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

  <div class="setup">
    <SpotifyConnectionCard
      {app}
      {scopeGap}
      {grantRevoked}
      redirectUri={data.redirectUri ?? ''}
      onRemoveApp={() => (connectionAction = 'clearApp')}
      onDisconnect={() => (connectionAction = 'disconnect')}
    />

    <SpotifyQueueCard
      {queue}
      updatedAt={queueAt}
      refreshing={queueRefreshing}
      {skipping}
      srEnabled={sr.enabled}
      redeemEnabled={redeem.enabled}
      onRefresh={() => refreshQueue(true)}
      onSkip={() => skipForm?.requestSubmit()}
      onEnableSr={enableSr}
    />

    <div class="paths" class:inspecting={inspector.isOpen}>
      <Card>
        <h2 class="path-title">{t('spotify.srTitle')}</h2>
        <p class="muted-text">{t('spotify.srHelp')}</p>
        <form method="POST" action="?/sr" use:enhance={srSubmit} bind:this={srForm}>
          {@render enableRow({
            label: t('spotify.srEnableLabel'),
            desc: sr.enabled ? t('spotify.srEnableOn') : t('spotify.srEnableOff'),
            descId: 'spotify-sr-desc',
            checked: sr.enabled,
            onchange: (on) => { sr.enabled = on; srChanged(); }
          })}
          {@render enableRow({
            label: t('spotify.liveOnlyLabel'),
            desc: sr.allowOffline ? t('spotify.srLiveOnlyOff') : t('spotify.srLiveOnlyOn'),
            descId: 'spotify-sr-live-desc',
            checked: !sr.allowOffline,
            onchange: (liveOnly) => { sr.allowOffline = !liveOnly; srChanged(); }
          })}
          <Field label={t('spotify.srPermLabel')}>
            <Select
              fill
              disabled={!sr.enabled}
              options={SPOTIFY_SR_PERMS.map((p) => ({ value: p, label: t(PERM_LABEL_KEYS[p]) }))}
              bind:value={() => sr.perm, (value) => (sr.perm = value as SpotifySrPerm)}
              onchange={srChanged}
            />
          </Field>
          <small class="reserved-hint" class:hidden={sr.enabled}>{t('spotify.srPermOffHint')}</small>
        </form>

        <form method="POST" action="?/quotas" use:enhance={quotasSubmit}>
          <h3 class="path-title quota-title">{t('spotify.quotaTitle')}</h3>
          <p class="muted-text">{t('spotify.quotaHelp')}</p>
          <div class="quota-grid">
            {#each SPOTIFY_QUOTA_TIERS as tier (tier)}
              <Field label={t(QUOTA_LABEL_KEYS[tier])}>
                <Input
                  fill
                  name={`quota_${tier}`}
                  type="number"
                  min="1"
                  step="1"
                  placeholder={t('spotify.quotaUnlimited')}
                  bind:value={quotaDraft[tier]}
                />
              </Field>
            {/each}
          </div>
          <Button variant="secondary" type="submit" loading={quotaSaving} disabled={!quotaDirty}>{t('spotify.quotaSave')}</Button>
        </form>
      </Card>

      <div class="redeem-col">
        <Card>
          <h2 class="path-title">{t('spotify.redeemTitle')}</h2>
          <p class="muted-text">{t('spotify.redeemHelp')}</p>
          <form method="POST" action="?/redeemToggle" use:enhance={redeemToggleSubmit} bind:this={redeemForm}>
            {@render enableRow({
              label: t('spotify.redeemEnableLabel'),
              desc: redeem.enabled ? t('spotify.redeemEnableOn') : t('spotify.redeemEnableOff'),
              descId: 'spotify-redeem-desc',
              checked: redeem.enabled,
              onchange: (on) => { redeem.enabled = on; redeemToggled(); }
            })}
            {@render enableRow({
              label: t('spotify.liveOnlyLabel'),
              desc: !redeem.enabled
                ? t('spotify.redeemLiveOnlyDisabled')
                : redeem.allowOffline ? t('spotify.redeemLiveOnlyOff') : t('spotify.redeemLiveOnlyOn'),
              descId: 'spotify-redeem-live-desc',
              checked: !redeem.allowOffline,
              disabled: !redeem.enabled,
              onchange: (liveOnly) => { redeem.allowOffline = !liveOnly; redeemToggled(); }
            })}
          </form>
          <div class="reward-slot">
            <SpotifyRewardRow
              {redeem}
              expanded={inspector.isOpen}
              onExpand={openReward}
              onDelete={() => (deletePending = true)}
            />
          </div>
        </Card>

        {#if inspector.isOpen && draft}
          <InspectorSurface
            open
            title={redeem.reward?.title || t('spotify.thisReward')}
            controls="spotify-editor"
            closeLabel={t('spotify.closeEditor')}
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
              <Scroller fill padding="16px" smooth>
                <SpotifyRewardEditor
                  bind:draft
                  canDelete={!isNew}
                  {busy}
                  attempted={validationAttempted}
                  onRequestDelete={() => (deletePending = true)}
                />
              </Scroller>
              <EditorFooter
                status={inspector.status}
                dirty={inspector.dirty}
                canSave={canSave && !busy}
                saveLabel={isNew ? t('spotify.create') : t('spotify.saveChanges')}
                cancelLabel={t('common.cancel')}
                savingLabel={t('spotify.saving')}
                savedLabel={t('spotify.saved')}
                errorLabel={t('spotify.toastSaveFailed')}
                dirtyLabel={t('spotify.unsavedChanges')}
                onCancel={closeInspector}
              />
            </form>
          </InspectorSurface>
        {/if}
      </div>
    </div>
  </div>

  {#if songCommands.length}
    <div class="cmd-block">
      <DeckList>
        <ModuleCommandList moduleId="songqueue" commands={songCommands} headingId="spotify-cmds-h" />
      </DeckList>
    </div>
  {/if}
</section>
{/if}

<ConfirmDialog
  open={discard.open}
  title={t('spotify.discardTitle')}
  body={t('spotify.discardBody')}
  confirmLabel={t('spotify.discard')}
  cancelLabel={t('spotify.keepEditing')}
  danger
  onCancel={discard.cancel}
  onConfirm={discard.confirm}
/>

<ConfirmDialog
  open={connectionAction !== null}
  title={t(connectionAction === 'clearApp' ? 'spotify.appRemoveTitle' : 'spotify.disconnectTitle')}
  body={t(connectionAction === 'clearApp' ? 'spotify.appRemoveWarning' : 'spotify.disconnectBody')}
  confirmLabel={t(connectionAction === 'clearApp' ? 'spotify.appRemove' : 'spotify.disconnect')}
  cancelLabel={t('spotify.appCancel')}
  busyLabel={t('spotify.saving')}
  danger
  busy={connectionBusy}
  onCancel={() => (connectionAction = null)}
  onConfirm={() => connectionForm?.requestSubmit()}
/>
<form method="POST" action="?/skip" use:enhance={skipSubmit} bind:this={skipForm} hidden></form>
<form method="POST" action={connectionAction === 'clearApp' ? '?/clearApp' : '?/disconnect'} use:enhance={connectionSubmit} bind:this={connectionForm} hidden></form>

<ConfirmDialog
  open={deletePending}
  title={t('spotify.deleteTitle')}
  body={t('spotify.deleteBody', { name: redeem.reward?.title || t('spotify.thisReward') })}
  confirmLabel={t('spotify.deleteConfirm')}
  cancelLabel={t('spotify.deleteCancel')}
  danger
  busy={deleting}
  onCancel={() => (deletePending = false)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/deleteReward" use:enhance={deleteSubmit} bind:this={deleteForm} hidden></form>

<style>
  .quota-title {
    margin-top: 18px;
  }
  .quota-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
    gap: 10px;
    margin-bottom: 10px;
  }

  .setup { display: grid; gap: 16px; }

  .path-title { margin: 0 0 6px; font-family: var(--bb-font-display); font-weight: 700; font-size: 15px; color: var(--bb-white); }
  .muted-text { color: var(--bb-muted); font-family: var(--bb-font-body); font-size: 13px; line-height: 1.55; margin: 0 0 14px; }
  .enable-text .muted-text { margin: 0; font-size: 12px; }

  .enable-row {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 14px;
  }
  .enable-text { display: grid; gap: 2px; flex: 1; min-width: 0; }
  .enable-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 13px; color: var(--bb-white); }

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
    .paths.inspecting { grid-template-columns: 1fr; }
    .paths.inspecting .redeem-col { grid-template-columns: minmax(0, 1fr) 440px; }
  }

  .reserved-hint { display: block; min-height: 16px; margin: -6px 0 14px; color: var(--bb-muted); font-family: var(--bb-font-body); font-size: 11.5px; }
  .reserved-hint.hidden { visibility: hidden; opacity: 0; }

  .inspector-form { display: flex; flex-direction: column; min-height: 0; flex: 1; }

  .cmd-block { margin-top: 32px; }
</style>
