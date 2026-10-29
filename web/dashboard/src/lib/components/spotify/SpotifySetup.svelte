<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { tick } from 'svelte';
  import { dev } from '$app/env';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { actionPayload, type ActionOk } from '@bagel/kit';
  import { copyFlash } from '@bagel/ui/lib/clipboard';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import MarkBadge from '@bagel/ui/svelte/MarkBadge.svelte';
  import SetupStage from '@bagel/ui/svelte/SetupStage.svelte';
  import SetupStep from '@bagel/ui/svelte/SetupStep.svelte';
  import SetupNote from '@bagel/ui/svelte/SetupNote.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import SetupAskAi from '../shared/SetupAskAi.svelte';

  let { name, app, redirectUri, preview = false, error = '', onRemoveApp }: {
    name: string; app: { present: boolean; clientId: string }; redirectUri: string;
    preview?: boolean; error?: string; onRemoveApp: () => void;
  } = $props();
  const { t } = getI18n();
  // Keep dev directly on each preview branch so production can erase the shortcuts.
  // Resume interrupted authorization at the account step.
  // svelte-ignore state_referenced_locally
  let step = $state(app.present ? 3 : 0);
  // svelte-ignore state_referenced_locally
  let furthest = $state(app.present ? 3 : 0);
  let saved = $state(false);
  let replacing = $state(false);
  const appReady = $derived(app.present || saved);
  const railSteps = $derived(['railWelcome', 'railApp', 'railKeys', 'railConnect'].map(key => ({ label: t(`spotifySetup.${key}`) })));
  const titles = ['welcomeTitle', 'appTitle', 'credentialsTitle', 'connectTitle'];
  const bodies = ['welcomeBody', 'appBody', 'credentialsBody', 'connectBody'];
  const faces = ['happy', 'curious', 'attentive', 'excited'];
  let dir = $state(1);
  let sequenceKey = $state(0);
  let done = $state(false);
  let clientId = $state('');
  let clientSecret = $state('');
  let saving = $state(false);
  let saveError = $state('');
  const message = $derived(saveError || error);
  const aiError = $derived(message && ![
    'spotify.app.redirectCopyFailed', 'spotify.app.saveFailed', 'spotify.errors.state',
    'spotify.errors.oauth', 'spotify.errors.declined', 'spotify.errors.noToken',
    'spotify.errors.noApp', 'spotify.errors.unconfigured', 'spotify.errors.store'
  ].some(key => message === t(key)) ? t('setupAi.genericError') : message);
  let copied = $state(false);
  let hover = $state(false);
  let heading = $state<HTMLHeadingElement | null>(null);
  const maxStep = $derived(appReady ? 3 : Math.min(furthest + 1, 2));
  const hero = $derived(step === 0 || done);
  async function go(target: number) {
    if (saving || done) return;
    target = Math.max(0, Math.min(target, maxStep));
    if (target === step) return;
    dir = target > step ? 1 : -1;
    step = target;
    furthest = Math.max(furthest, step);
    sequenceKey += 1;
    await tick();
    heading?.focus({ preventScroll: true });
  }
  async function copyRedirect() {
    if (!(await copyFlash(redirectUri, (on) => copied = on, 2200))) saveError = t('spotify.app.redirectCopyFailed');
  }
  function startReplace() {
    clientId = app.clientId || clientId;
    clientSecret = '';
    saveError = '';
    replacing = true;
  }
  function cancelReplace() {
    clientSecret = '';
    saveError = '';
    replacing = false;
  }
  function removeApp() {
    saved = false;
    onRemoveApp();
  }
  const submit: SubmitFunction = () => {
    saving = true;
    saveError = '';
    return async ({ result }) => {
      saving = false;
      const payload = actionPayload<ActionOk>(result);
      if (result.type !== 'success' || payload?.ok !== true) {
        saveError = payload?.error ?? t('spotify.app.saveFailed');
        return;
      }
      clientSecret = '';
      saved = true;
      replacing = false;
      await invalidateAll();
      await go(3);
    };
  };
  function usePreviewKeys() {
    if (!dev || !preview) return;
    clientSecret = '';
    clientId = 'preview-client-id';
    saved = true;
    void go(3);
  }
</script>

<SetupStage
  label={t('spotify.connection.connectTitle')}
  steps={railSteps}
  current={step}
  {maxStep}
  stepLabel={t('onboarding.stepOf', { n: step + 1, total: railSteps.length })}
  onSelect={go}
  {hero}
  style="--setup-stage-offset: 76px"
  data-orbs="off"
  data-canvas="fill"
>
  {#snippet start()}<TextLink variant="quiet" icon="arrowLeft" href="/modules" label={t('spotify.back')} />{/snippet}
  {#snippet art()}
    <Bolota {name} size={240} active follow cycle={false}
      expression={done || copied ? 'proud' : hover ? 'excited' : faces[step]}
      sequence={done ? 'burst' : 'entrance'} {sequenceKey} sequenceFor={1100} sequenceHold={240} />
  {/snippet}
  {#snippet aside()}
    {#if !done}<SetupAskAi feature="spotify" step={railSteps[step].label} stepIndex={step} error={aiError} {redirectUri} />{/if}
  {/snippet}
  {#snippet foot()}
    <Text as="span" size="xs" mono tone="muted">{dev && preview ? t('spotifySetup.preview') : t('spotifySetup.footer')}</Text>
  {/snippet}
  {#snippet end()}<TextLink variant="quiet" href="/modules" label={`${t('spotifySetup.later')} ↗`} />{/snippet}
  {#key `${step}-${done}`}
    <SetupStep
      kicker={`${t('spotify.eyebrow')} / ${String(step + 1).padStart(2, '0')}`}
      title={t(`spotifySetup.${done ? 'readyTitle' : titles[step]}`)}
      body={t(`spotifySetup.${done ? 'readyBody' : bodies[step]}`)}
      {dir}
      {hero}
      bind:heading
    >
      {#if done}
        <div class="bb-setup-actions"><ButtonLink href="/songqueue">{t('spotifySetup.readyCta')}</ButtonLink></div>
      {:else if step === 0}
        <SetupNote title={t('spotifySetup.premiumTitle')} body={t('spotifySetup.premiumBody')}>
          {#snippet icon()}♪{/snippet}
        </SetupNote>
        <div class="bb-setup-actions"><Button onclick={() => go(1)} onpointerenter={() => hover = true} onpointerleave={() => hover = false}>{t('spotifySetup.start')} →</Button></div>
      {:else if step === 1}
        <div class="block">
          <Label mono as="span">{t('spotifySetup.redirectLabel')}</Label>
          {#if redirectUri}<div class="bb-setup-row"><span class="redirect-value"><Text as="span" size="xs" mono tone="pale">{redirectUri}</Text></span><Button variant="ghost" size="sm" onclick={copyRedirect}>{copied ? t('spotify.app.redirectCopied') : t('spotify.app.redirectCopy')}</Button></div>
          {:else}<Text size="sm" tone="muted">{t('spotifySetup.redirectMissing')}</Text>{/if}
        </div>
        <div class="bb-setup-actions">
          <Button variant="ghost" onclick={() => go(0)}>{t('onboarding.back')}</Button>
          <ButtonLink href="https://developer.spotify.com/dashboard" target="_blank" rel="noopener noreferrer" variant="secondary">{t('spotifySetup.developer')} ↗</ButtonLink>
          <Button onclick={() => go(2)} disabled={!redirectUri}>{t('spotifySetup.appCreated')} →</Button>
        </div>
      {:else if step === 2}
        {#if appReady && !replacing}
          <div class="ready">
            <MarkBadge size="xs" tone="success" aria-hidden="true"><Icon name="check" size={14} /></MarkBadge>
            <Text as="span" size="sm">{t('spotifySetup.saved')}</Text>
            <Text as="span" size="xs" mono tone="muted">{app.clientId || clientId}</Text>
          </div>
          <div class="bb-setup-actions">
            <Button variant="ghost" onclick={() => go(1)}>{t('onboarding.back')}</Button>
            <Button variant="secondary" onclick={startReplace}>{t('spotify.app.replace')}</Button>
            {#if app.present}<Button onclick={removeApp} tone="danger">{t('spotify.app.remove')}</Button>{/if}
            <Button onclick={() => go(3)}>{t('onboardingImport.continue')} →</Button>
          </div>
        {:else}
          <form method="POST" action="?/saveApp" use:enhance={submit}>
            <Field label={t('spotify.app.clientIdLabel')}><Input fill mono name="client_id" bind:value={clientId} autocomplete="off" spellcheck="false" required /></Field>
            <Field label={t('spotify.app.clientSecretLabel')}><Input fill mono name="client_secret" type="password" bind:value={clientSecret} autocomplete="off" spellcheck="false" required /></Field>
            <Text size="xs" tone="muted">{t('spotify.app.clientSecretHint')}</Text>
            <div class="bb-setup-actions">
              {#if replacing}<Button variant="ghost" onclick={cancelReplace} disabled={saving}>{t('spotify.app.cancel')}</Button>
              {:else}<Button variant="ghost" onclick={() => go(1)} disabled={saving}>{t('onboarding.back')}</Button>{/if}
              <Button type="submit" busy={saving}>{t('spotifySetup.saveContinue')} →</Button>
            </div>
          </form>
          {#if dev && preview}<div class="bb-setup-actions"><Button variant="ghost" onclick={usePreviewKeys}>{t('spotifySetup.previewKeys')} →</Button></div>{/if}
        {/if}
      {:else}
        <div class="ready">
          <MarkBadge size="xs" tone="success" aria-hidden="true"><Icon name="check" size={14} /></MarkBadge>
          <Text as="span" size="sm">{t('spotifySetup.saved')}</Text>
        </div>
        <div class="bb-setup-actions"><Button variant="ghost" onclick={() => go(2)}>{t('onboarding.back')}</Button>
          <ButtonLink href="/spotify/connect" data-sveltekit-reload>{t('spotify.connection.connectCta')} →</ButtonLink>
        </div>
      {/if}
      {#snippet status()}
        {#if message}<AlertBanner tone={saveError ? 'danger' : 'warning'} flush>{message}</AlertBanner>{/if}
      {/snippet}
    </SetupStep>
  {/key}
</SetupStage>

<style>
  .ready { display: flex; flex-wrap: wrap; align-items: center; justify-content: center; gap: 6px 10px; }
  .block { display: grid; gap: 8px; }
  .redirect-value { flex: 1; min-width: 0; user-select: all; }
  form { --field-mb: 0; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
  form > :global(:nth-child(n+3)) { grid-column: 1 / -1; }
</style>
