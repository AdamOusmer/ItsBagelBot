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
  import { parallax } from '@bagel/ui/svelte/actions';
  import AmbientSky from '@bagel/ui/svelte/AmbientSky.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Stepper from '@bagel/ui/svelte/Stepper.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';

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
  let copied = $state(false);
  let px = $state(0);
  let py = $state(0);
  let hover = $state(false);
  let heading = $state<HTMLHeadingElement | null>(null);
  const blobRight = $derived(step % 2 === 1);
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
  function follow(point: { px: number; py: number }) {
    px = point.px;
    py = point.py;
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

<div class="spotify-setup" role="region" aria-label={t('spotify.connection.connectTitle')} data-orbs="off" data-canvas="fill" use:parallax={{ onmove: follow }}>
  <AmbientSky position="contained" shift={blobRight ? 1 : -1} turn={step * 24} {px} {py} progress={done ? 1 : step / 3} leaving={done} />
  <header class="top">
    <TextLink variant="quiet" icon="arrowLeft" href="/modules" label={t('spotify.back')} />
    <Stepper compact steps={railSteps} current={step} {maxStep} label={t('onboarding.stepOf', { n: step + 1, total: 4 })} onselect={go} />
  </header>
  <div class="stage">
    <div class="pair">
      <div class="blob-col" class:right={blobRight}>
        <div class="scale" class:hero={step === 0 || done}>
          <div class="float"><div class="plate" aria-hidden="true"></div>
            <Bolota {name} size={240} active follow cycle={false}
              expression={done ? 'proud' : copied ? 'proud' : hover ? 'excited' : faces[step]}
              sequence={done ? 'burst' : 'entrance'} {sequenceKey} sequenceFor={1100} sequenceHold={240} />
          </div>
        </div>
      </div>
      <div class="copy-col" class:left={blobRight}>
        {#key `${step}-${done}`}
          <section class="step">
            <div class="kicker" in:arrive={{ i: 0 }} out:depart={{ i: 0 }}><Eyebrow as="p">{t('spotify.eyebrow')} <span class="kicker-step"> / {String(step + 1).padStart(2, '0')}</span></Eyebrow></div>
            <h1 class="title" tabindex="-1" bind:this={heading} in:arrive={{ i: 1 }} out:depart={{ i: 1 }}>{t(`spotifySetup.${done ? 'readyTitle' : titles[step]}`)}</h1>
            <div class="body" in:arrive={{ i: 2 }} out:depart={{ i: 2 }}><Text tone="muted">{t(`spotifySetup.${done ? 'readyBody' : bodies[step]}`)}</Text></div>
            <div class="controls" in:arrive={{ i: 3 }} out:depart={{ i: 3 }}>
              {#if error}<AlertBanner variant="warn">{error}</AlertBanner>{/if}
              {#if saveError}<AlertBanner variant="danger">{saveError}</AlertBanner>{/if}
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
                    {#if app.present}<Button variant="destructive" onclick={removeApp}>{t('spotify.app.remove')}</Button>{/if}
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
                      <Button type="submit" loading={saving}>{t('spotifySetup.saveContinue')} →</Button>
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
          </section>
        {/key}
      </div>
    </div>
  </div>
  <footer><Text as="span" size="xs" mono tone="muted">{dev && preview ? t('spotifySetup.preview') : t('spotifySetup.footer')}</Text><TextLink variant="quiet" href="/modules" label={`${t('spotifySetup.later')} ↗`} /></footer>
</div>

<style>
  .spotify-setup { --gap: clamp(24px, 4vw, 64px); --setup-gutter: clamp(24px, 4vw, 56px); position: relative; isolation: isolate; width: 100%; min-height: calc(100svh - 76px); display: grid; grid-template-rows: auto 1fr auto; overflow: clip; }
  .top, .stage, footer { position: relative; z-index: 1; }
  .top { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 24px var(--setup-gutter); }
  .stage { display: grid; place-items: center; padding: 20px var(--setup-gutter) 40px; }
  .pair { width: min(100%, 1080px); display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: var(--gap); min-height: 460px; align-items: start; }
  .blob-col, .copy-col { transition: transform calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo); min-width: 0; }
  .blob-col { display: flex; justify-content: center; align-self: center; }
  .blob-col.right { transform: translateX(calc(100% + var(--gap))); }
  .copy-col.left { transform: translateX(calc(-100% - var(--gap))); }
  .scale { transform: scale(.86); transition: transform calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo); }
  .scale.hero { transform: scale(1.1); }
  .float { position: relative; filter: drop-shadow(0 18px 34px rgba(var(--bb-shadow-rgb), .42)); animation: float 9s ease-in-out infinite; }
  .plate { position: absolute; inset: -22%; border-radius: 50%; background: radial-gradient(circle at 40% 35%, rgba(var(--bb-green-glow-rgb), .34), rgba(var(--bb-tan-rgb), .16) 46%, transparent 70%); filter: blur(22px); animation: breathe 6s ease-in-out infinite; z-index: -1; }
  .copy-col { display: grid; padding-top: 48px; }
  .step { grid-area: 1 / 1; min-width: 0; }
  .kicker { margin-bottom: 18px; }
  .kicker-step { color: var(--bb-muted); }
  .title { margin: 0 0 22px; font: 700 clamp(2rem, 3.8vw, 3.5rem)/1.04 var(--bb-font-display); letter-spacing: -.035em; outline: none; background: linear-gradient(100deg, var(--bb-white) 0 40%, var(--bb-tan-pale) 50%, var(--bb-white) 60% 100%); background-size: 260% 100%; background-position: 120% 0; background-clip: text; -webkit-text-fill-color: transparent; animation: sheen 1.8s var(--bb-ease-out-expo) 520ms both; }
  .body { max-width: 46ch; }
  .controls { margin-top: 26px; overflow-wrap: anywhere; }
  .premium-note { display: grid; gap: 6px; padding: 14px 16px; background: rgba(var(--bb-tan-rgb), .06); border-radius: var(--bb-radius-md); margin-bottom: 14px; }
  .promise { display: flex; align-items: center; gap: 12px; padding: 12px 0; }
  .promise-glyph { color: var(--bb-green-glow); font-size: 26px; }
  .promise i { font-style: normal; padding: 0 8px; color: var(--bb-muted); }
  .actions { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; margin-top: 26px; }
  form { display: grid; gap: 16px; }
  form .actions { margin-top: 8px; }
  .preview-action { margin-top: 16px; }
  .redirect-block { display: grid; gap: 8px; margin-top: 24px; }
  .redirect { border-block: 1px solid var(--bb-border); padding: 12px 0; display: flex; gap: 8px; align-items: center; }
  .redirect-value { flex: 1; min-width: 0; user-select: all; }
  .preview-hint { margin-top: 16px; }
  .saved { margin-block: 13px; }
  footer { padding: 20px var(--setup-gutter) 28px; display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 16px; }
  @keyframes float { 0%, 100% { transform: translateY(0) rotate(-2deg); } 50% { transform: translateY(-12px) rotate(2deg); } }
  @keyframes breathe { 0%, 100% { opacity: .7; transform: scale(.94); } 50% { opacity: 1; transform: scale(1.06); } }
  @keyframes sheen { from { background-position: 120% 0; } to { background-position: -20% 0; } }
  @media (max-width: 1050px) and (min-width: 761px) { .scale.hero { transform: scale(.88); } .scale { transform: scale(.72); } .title { font-size: 2.4rem; } }
  @media (max-width: 760px) {
    .spotify-setup { min-height: calc(100svh - 68px); padding-bottom: 92px; }
    .top { padding-top: 20px; }
    .pair { grid-template-columns: minmax(0, 1fr); min-height: 0; gap: 12px; }
    .blob-col, .blob-col.right { height: 190px; transform: none; }
    .scale, .scale.hero { transform: scale(.68); }
    .copy-col, .copy-col.left { transform: none; padding-top: 0; width: min(100%, 520px); justify-self: center; }
    .title { font-size: clamp(2rem, 7vw, 3rem); }
    .stage { padding-top: 0; }
    footer { padding-top: 12px; }
  }
  @media (prefers-reduced-motion: reduce) {
    .blob-col, .copy-col, .scale { transition: none; }
    .float, .plate, .title { animation: none; }
  }
</style>
