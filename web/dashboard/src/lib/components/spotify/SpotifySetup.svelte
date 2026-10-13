<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { tick } from 'svelte';
  import { fly } from 'svelte/transition';
  import { dev } from '$app/environment';
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { actionPayload, type ActionOk } from '@bagel/kit';
  import { copyFlash } from '@bagel/ui/lib/clipboard';
  import { bezier } from '@bagel/ui/lib/tween';
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import SetupStage from '../shared/SetupStage.svelte';
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
  const expo = bezier(0.16, 1, 0.3, 1);
  const arrive = (node: Element, { i = 0 } = {}) => prefersReducedMotion() ? { duration: 0 } :
    fly(node, { x: dir * 72 * (i % 2 ? -1 : 1), duration: 760, delay: 160 + i * 70, easing: expo, opacity: 0 });
  const depart = (node: Element, { i = 0 } = {}) => prefersReducedMotion() ? { duration: 0 } :
    fly(node, { x: -dir * 56 * (i % 2 ? -1 : 1), duration: 380, delay: i * 35, easing: expo, opacity: 0 });
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
  async function previewConnect() {
    if (!dev || !preview) return;
    done = true;
    sequenceKey += 1;
    await tick();
    heading?.focus({ preventScroll: true });
  }
</script>

<SetupStage label={t('spotify.connection.connectTitle')} {name} {step} {done} {railSteps} {maxStep} onSelect={go} expression={done ? 'proud' : copied ? 'proud' : hover ? 'excited' : faces[step]} {sequenceKey} backLabel={t('spotify.back')} footer={dev && preview ? t('spotifySetup.preview') : t('spotifySetup.footer')} later={t('spotifySetup.later')}>
  {#snippet aside()}
    {#if !done}<SetupAskAi feature="spotify" step={railSteps[step].label} stepIndex={step} error={aiError} {redirectUri} />{/if}
  {/snippet}
  {#key `${step}-${done}`}
    <section class="step">
      <div class="kicker" in:arrive={{ i: 0 }} out:depart={{ i: 0 }}><Eyebrow as="p">{t('spotify.eyebrow')} <span class="kicker-step"> / {String(step + 1).padStart(2, '0')}</span></Eyebrow></div>
      <h1 class="title" tabindex="-1" bind:this={heading} in:arrive={{ i: 1 }} out:depart={{ i: 1 }}>{t(`spotifySetup.${done ? 'readyTitle' : titles[step]}`)}</h1>
      <div class="body" in:arrive={{ i: 2 }} out:depart={{ i: 2 }}><Text tone="muted">{t(`spotifySetup.${done ? 'readyBody' : bodies[step]}`)}</Text></div>
      <div class="controls" in:arrive={{ i: 3 }} out:depart={{ i: 3 }}>
        {#if done}
          <ButtonLink href="/songqueue">{t('spotifySetup.readyCta')}</ButtonLink>
        {:else if step === 0}
          <div class="premium-note"><Text as="span" size="sm" tone="pale"><strong>{t('spotifySetup.premiumTitle')}</strong></Text><Text size="xs" tone="muted">{t('spotifySetup.premiumBody')}</Text></div>
          <div class="promise"><span class="promise-glyph" aria-hidden="true">♪</span><Text as="span" size="sm" tone="pale">Spotify <i>×</i> ItsBagelBot</Text></div>
          <div class="actions"><Button onclick={() => go(1)} onpointerenter={() => hover = true} onpointerleave={() => hover = false}>{t('spotifySetup.start')} →</Button></div>
        {:else if step === 1}
          <ButtonLink href="https://developer.spotify.com/dashboard" target="_blank" rel="noopener noreferrer" variant="secondary">{t('spotifySetup.developer')} ↗</ButtonLink>
          <div class="redirect-block"><Label mono as="span">{t('spotifySetup.redirectLabel')}</Label>
            {#if redirectUri}<div class="redirect"><span class="redirect-value"><Text as="span" size="xs" mono tone="pale">{redirectUri}</Text></span><Button variant="ghost" size="sm" onclick={copyRedirect}>{copied ? t('spotify.app.redirectCopied') : t('spotify.app.redirectCopy')}</Button></div>
            {:else}<Text size="sm" tone="muted">{t('spotifySetup.redirectMissing')}</Text>{/if}
          </div>
          <div class="actions"><Button variant="ghost" onclick={() => go(0)}>{t('onboarding.back')}</Button><Button onclick={() => go(2)} disabled={!redirectUri}>{t('spotifySetup.appCreated')} →</Button></div>
        {:else if step === 2}
          {#if appReady && !replacing}
            <div class="saved"><Tag tone="live" mark="solid">{t('spotifySetup.saved')}</Tag></div><Text as="span" size="xs" mono tone="pale">{app.clientId || clientId}</Text>
            <div class="actions">
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
              <div class="actions">
                {#if replacing}<Button variant="ghost" onclick={cancelReplace} disabled={saving}>{t('spotify.app.cancel')}</Button>
                {:else}<Button variant="ghost" onclick={() => go(1)} disabled={saving}>{t('onboarding.back')}</Button>{/if}
                <Button type="submit" busy={saving}>{t('spotifySetup.saveContinue')} →</Button>
              </div>
            </form>
            {#if dev && preview}<div class="preview-action"><Button variant="ghost" onclick={usePreviewKeys}>{t('spotifySetup.previewKeys')} →</Button></div>{/if}
          {/if}
        {:else}
          <div class="saved"><Tag tone="live" mark="solid">{t('spotifySetup.saved')}</Tag></div>
          <div class="actions"><Button variant="ghost" onclick={() => go(2)}>{t('onboarding.back')}</Button>
            {#if dev && preview}<Button onclick={previewConnect}>{t('spotifySetup.previewConnect')} →</Button>
            {:else}<ButtonLink href="/spotify/connect" data-sveltekit-reload>{t('spotify.connection.connectCta')} →</ButtonLink>{/if}
          </div>
          {#if dev && preview}<div class="preview-hint"><Text size="xs" tone="muted">{t('spotifySetup.previewHint')}</Text></div>{/if}
        {/if}
      </div>
      <div class="message-slot" in:arrive={{ i: 4 }} out:depart={{ i: 4 }}>
        {#if message}<AlertBanner tone={saveError ? 'danger' : 'warning'} flush>{message}</AlertBanner>{/if}
      </div>
    </section>
  {/key}
</SetupStage>

<style>
  .step { grid-area: 1 / 1; min-width: 0; }
  .kicker { margin-bottom: var(--setup-kicker-gap); }
  .kicker-step { color: var(--bb-muted); }
  .title { margin: 0 0 var(--setup-title-gap); font: 700 var(--setup-title-size)/1.04 var(--bb-font-display); letter-spacing: -.035em; outline: none; background: linear-gradient(100deg, var(--bb-white) 0 40%, var(--bb-tan-pale) 50%, var(--bb-white) 60% 100%); background-size: 260% 100%; background-position: 120% 0; background-clip: text; -webkit-text-fill-color: transparent; animation: sheen 1.8s var(--bb-ease-out-expo) 520ms both; }
  .body { max-width: 46ch; }
  .controls { margin-top: var(--setup-controls-gap); overflow-wrap: anywhere; }
  .message-slot { min-height: calc(2lh + 10px); margin-top: 10px; font-size: 12px; line-height: 1.4; overflow-wrap: anywhere; }
  .premium-note { display: grid; gap: 6px; padding: min(14px, 1vw, 1.5vh) 16px; background: rgba(var(--bb-tan-rgb), .06); border-radius: var(--bb-radius-md); margin-bottom: min(14px, 1vw, 1.5vh); }
  .promise { display: flex; align-items: center; gap: 12px; padding: min(12px, 1vw, 1.3vh) 0; }
  .promise-glyph { color: var(--bb-green-glow); font-size: 26px; }
  .promise i { font-style: normal; padding: 0 8px; color: var(--bb-muted); }
  .actions { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; margin-top: var(--setup-actions-gap); }
  form { --field-mb: 0; display: grid; gap: min(16px, 1.2vw, 1.2vh); }
  form .actions { margin-top: 8px; }
  .preview-action { margin-top: min(16px, 1.2vw, 1.2vh); }
  .redirect-block { display: grid; gap: 8px; margin-top: min(24px, 1.8vw, 1.8vh); }
  .redirect { border-block: 1px solid var(--bb-border); padding: min(12px, 1vw, 1.3vh) 0; display: flex; gap: 8px; align-items: center; }
  .redirect-value { flex: 1; min-width: 0; user-select: all; }
  .preview-hint { margin-top: min(16px, 1.2vw, 1.2vh); }
  .saved { margin-block: 13px; }
  @keyframes sheen { from { background-position: 120% 0; } to { background-position: -20% 0; } }
  @media (min-width: 761px) and (max-height: 860px), (max-width: 760px) {
    .body { --bb-text-md: 14px; }
    .premium-note { gap: 4px; padding: 10px 12px; margin-bottom: 8px; }
    .promise { padding: 6px 0; }
    form { --field-mb: 0; gap: 10px; }
    .preview-action, .preview-hint { margin-top: 10px; }
    .redirect-block { margin-top: 14px; gap: 6px; }
    .redirect { padding-block: 8px; }
  }
  @media (min-width: 761px) and (max-height: 760px), (max-width: 760px) {
    .premium-note { padding: 8px 10px; }
    .promise { padding: 4px 0; }
    .redirect-block { margin-top: 10px; }
    form { gap: 8px; }
    form .actions { margin-top: 4px; }
    .preview-action, .preview-hint { margin-top: 8px; }
    .saved { margin-block: 8px; }
  }
  @media (max-width: 760px) {
    form { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    form > :global(:nth-child(n+3)) { grid-column: 1 / -1; }
    form .actions { margin-top: 0; }
  }
  @media (prefers-reduced-motion: reduce) { .title { animation: none; } }
</style>
