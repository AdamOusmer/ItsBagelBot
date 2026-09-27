<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  import { onDestroy, tick } from 'svelte';
  import { fly } from 'svelte/transition';
  import { dev } from '$app/environment';
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { actionPayload, type ActionOk } from '@bagel/kit';
  import { copyText } from '@bagel/ui/lib/clipboard';
  import { bezier } from '@bagel/ui/lib/tween';
  import { hasFinePointer, prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import AmbientSky from '@bagel/ui/svelte/AmbientSky.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import StepRail from '$lib/components/welcome/StepRail.svelte';

  let { name, app, redirectUri, preview = false, degraded = false, error = '' }: {
    name: string; app: { present: boolean; clientId: string }; redirectUri: string;
    preview?: boolean; degraded?: boolean; error?: string;
  } = $props();
  const { t } = getI18n();
  // Keep dev directly on each preview branch so production can erase the shortcuts.
  // Resume interrupted authorization at the account step.
  // svelte-ignore state_referenced_locally
  let step = $state(app.present ? 3 : 0);
  // svelte-ignore state_referenced_locally
  let furthest = $state(app.present ? 3 : 0);
  let saved = $state(false);
  const appReady = $derived(app.present || saved);
  const labels = $derived(['railWelcome', 'railApp', 'railKeys', 'railConnect'].map(key => t(`spotifySetup.${key}`)));
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
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  let px = $state(0);
  let py = $state(0);
  let pointerFrame = 0;
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
  function movePointer(e: PointerEvent) {
    if (!hasFinePointer() || prefersReducedMotion() || pointerFrame) return;
    const bounds = e.currentTarget instanceof HTMLElement ? e.currentTarget.getBoundingClientRect() : null;
    if (!bounds) return;
    pointerFrame = requestAnimationFrame(() => {
      pointerFrame = 0;
      px = ((e.clientX - bounds.left) / bounds.width - .5) * 2;
      py = ((e.clientY - bounds.top) / bounds.height - .5) * 2;
    });
  }
  async function copyRedirect() {
    if (await copyText(redirectUri)) {
      copied = true;
      clearTimeout(copyTimer);
      copyTimer = setTimeout(() => copied = false, 2200);
    } else saveError = t('spotify.redirectCopyFailed');
  }
  const submit: SubmitFunction = () => {
    saving = true;
    saveError = '';
    return async ({ result }) => {
      saving = false;
      const payload = actionPayload<ActionOk>(result);
      if (result.type !== 'success' || payload?.ok !== true) {
        saveError = payload?.error ?? t('spotify.appSaveFailed');
        return;
      }
      clientSecret = '';
      saved = true;
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
  onDestroy(() => {
    clearTimeout(copyTimer);
    if (pointerFrame) cancelAnimationFrame(pointerFrame);
  });
</script>

<div class="spotify-setup" role="region" aria-label={t('spotify.connectTitle')} data-spotify-setup onpointermove={movePointer} onpointerleave={() => { px = 0; py = 0; }}>
  <AmbientSky position="contained" shift={blobRight ? 1 : -1} turn={step * 24} {px} {py} progress={done ? 1 : step / 3} leaving={done} />
  <header class="top">
    <a class="back" href="/modules">← {t('spotify.back')}</a>
    <StepRail {labels} current={step} {maxStep} label={t('onboarding.stepOf', { n: step + 1, total: 4 })} onselect={go} />
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
            <p class="kicker" in:arrive={{ i: 0 }} out:depart={{ i: 0 }}>{t('spotify.eyebrow')} <span> / {String(step + 1).padStart(2, '0')}</span></p>
            <h1 class="title" tabindex="-1" bind:this={heading} in:arrive={{ i: 1 }} out:depart={{ i: 1 }}>{t(`spotifySetup.${done ? 'readyTitle' : titles[step]}`)}</h1>
            <p class="body" in:arrive={{ i: 2 }} out:depart={{ i: 2 }}>{t(`spotifySetup.${done ? 'readyBody' : bodies[step]}`)}</p>
            <div class="controls" in:arrive={{ i: 3 }} out:depart={{ i: 3 }}>
              {#if degraded}<AlertBanner>{t('spotify.degraded')}</AlertBanner>{/if}
              {#if error}<AlertBanner variant="warn">{error}</AlertBanner>{/if}
              {#if saveError}<p class="error" role="alert">{saveError}</p>{/if}
              {#if done}
                <ButtonLink href="/songqueue">{t('spotifySetup.readyCta')}</ButtonLink>
              {:else if step === 0}
                <div class="premium-note"><strong>{t('spotifySetup.premiumTitle')}</strong><p>{t('spotifySetup.premiumBody')}</p></div>
                <div class="promise"><span aria-hidden="true">♪</span><span>Spotify <i>×</i> ItsBagelBot</span></div>
                <div class="actions"><Button onclick={() => go(1)} onpointerenter={() => hover = true} onpointerleave={() => hover = false}>{t('spotifySetup.start')} →</Button></div>
              {:else if step === 1}
                <ButtonLink href="https://developer.spotify.com/dashboard" target="_blank" rel="noopener noreferrer" variant="secondary">{t('spotifySetup.developer')} ↗</ButtonLink>
                <div class="redirect-block"><span class="field-label">Redirect URI</span>
                  {#if redirectUri}<div class="redirect"><code>{redirectUri}</code><Button variant="ghost" size="sm" onclick={copyRedirect}>{copied ? t('spotify.redirectCopied') : t('spotify.redirectCopy')}</Button></div>
                  {:else}<p class="body small">{t('spotifySetup.redirectMissing')}</p>{/if}
                </div>
                <div class="actions"><Button variant="ghost" onclick={() => go(0)}>{t('onboarding.back')}</Button><Button onclick={() => go(2)} disabled={!redirectUri}>{t('spotifySetup.appCreated')} →</Button></div>
              {:else if step === 2}
                {#if appReady}
                  <p class="saved">✓ {t('spotifySetup.saved')}</p><code>{app.clientId || clientId}</code>
                  <div class="actions"><Button variant="ghost" onclick={() => go(1)}>{t('onboarding.back')}</Button><Button onclick={() => go(3)}>{t('onboardingImport.continue')} →</Button></div>
                {:else}
                  <form method="POST" action="?/saveApp" use:enhance={submit}>
                    <Field label={t('spotify.appClientIdLabel')}><Input fill mono name="client_id" bind:value={clientId} autocomplete="off" spellcheck="false" required /></Field>
                    <Field label={t('spotify.appClientSecretLabel')}><Input fill mono name="client_secret" type="password" bind:value={clientSecret} autocomplete="off" spellcheck="false" required /></Field>
                    <p class="hint">{t('spotify.appClientSecretHint')}</p>
                    <div class="actions"><Button variant="ghost" onclick={() => go(1)} disabled={saving}>{t('onboarding.back')}</Button><Button type="submit" loading={saving}>{t('spotifySetup.saveContinue')} →</Button></div>
                  </form>
                  {#if dev && preview}<div class="preview-action"><Button variant="ghost" onclick={usePreviewKeys}>{t('spotifySetup.previewKeys')} →</Button></div>{/if}
                {/if}
              {:else}
                <p class="saved">✓ {t('spotifySetup.saved')}</p>
                <div class="actions"><Button variant="ghost" onclick={() => go(2)}>{t('onboarding.back')}</Button>
                  {#if dev && preview}<Button onclick={previewConnect}>{t('spotifySetup.previewConnect')} →</Button>
                  {:else}<ButtonLink href="/spotify/connect" data-sveltekit-reload>{t('spotify.connectCta')} →</ButtonLink>{/if}
                </div>
                {#if dev && preview}<p class="hint">{t('spotifySetup.previewHint')}</p>{/if}
              {/if}
            </div>
          </section>
        {/key}
      </div>
    </div>
  </div>
  <footer><span>{dev && preview ? t('spotifySetup.preview') : t('spotifySetup.footer')}</span><a href="/modules">{t('spotifySetup.later')} ↗</a></footer>
</div>

<style>
  :global(body:has([data-spotify-setup]) .bb-bg-orb) { display: none; }
  :global(.bb-shell__canvas:has([data-spotify-setup])) { max-width: none; padding: 0; flex: 1; display: flex; }
  .spotify-setup { --gap: clamp(24px, 4vw, 64px); --setup-gutter: clamp(24px, 4vw, 56px); position: relative; isolation: isolate; width: 100%; min-height: calc(100svh - 76px); display: grid; grid-template-rows: auto 1fr auto; overflow: clip; }
  .top, .stage, footer { position: relative; z-index: 1; }
  .top { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 24px var(--setup-gutter); }
  .back { margin: 0; font: 12px var(--bb-font-body); color: var(--bb-muted); text-decoration: none; }
  .stage { display: grid; place-items: center; padding: 20px var(--setup-gutter) 40px; }
  .pair { width: min(100%, 1080px); display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: var(--gap); min-height: 460px; align-items: start; }
  .blob-col, .copy-col { transition: transform 1000ms var(--bb-ease-out-expo); min-width: 0; }
  .blob-col { display: flex; justify-content: center; align-self: center; }
  .blob-col.right { transform: translateX(calc(100% + var(--gap))); }
  .copy-col.left { transform: translateX(calc(-100% - var(--gap))); }
  .scale { transform: scale(.86); transition: transform 1000ms var(--bb-ease-out-expo); }
  .scale.hero { transform: scale(1.1); }
  .float { position: relative; filter: drop-shadow(0 18px 34px rgba(0,0,0,.42)); animation: float 9s ease-in-out infinite; }
  .plate { position: absolute; inset: -22%; border-radius: 50%; background: radial-gradient(circle at 40% 35%, rgba(var(--bb-green-glow-rgb), .34), rgba(var(--bb-tan-rgb), .16) 46%, transparent 70%); filter: blur(22px); animation: breathe 6s ease-in-out infinite; z-index: -1; }
  .copy-col { display: grid; padding-top: 48px; }
  .step { grid-area: 1 / 1; min-width: 0; }
  .kicker { margin: 0 0 18px; font: 11px var(--bb-font-mono); letter-spacing: .14em; text-transform: uppercase; color: var(--bb-tan); }
  .kicker span { color: var(--bb-muted); }
  .title { margin: 0 0 22px; font: 700 clamp(2rem, 3.8vw, 3.5rem)/1.04 var(--bb-font-display); letter-spacing: -.035em; outline: none; background: linear-gradient(100deg, var(--bb-white) 0 40%, var(--bb-tan-pale) 50%, var(--bb-white) 60% 100%); background-size: 260% 100%; background-position: 120% 0; background-clip: text; -webkit-text-fill-color: transparent; animation: sheen 1.8s var(--bb-ease-out-expo) 520ms both; }
  .body { margin: 0; max-width: 46ch; font: 16px/1.65 var(--bb-font-body); color: var(--bb-muted); }
  .controls { margin-top: 26px; }
  .premium-note { padding: 14px 16px; background: rgba(var(--bb-tan-rgb), .06); border-radius: var(--bb-radius-md); margin-bottom: 14px; }
  .premium-note strong { font: 600 13px var(--bb-font-body); color: var(--bb-tan-pale); }
  .premium-note p { margin: 6px 0 0; font: 12px/1.6 var(--bb-font-body); color: var(--bb-muted); }
  .promise { display: flex; align-items: center; gap: 12px; padding: 12px 0; color: var(--bb-tan-pale); font: 13px var(--bb-font-body); }
  .promise > span:first-child { color: var(--bb-green-glow); font-size: 26px; }
  .promise i { font-style: normal; padding: 0 8px; color: var(--bb-muted); }
  .actions { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; margin-top: 26px; }
  form { display: grid; gap: 16px; }
  form .actions { margin-top: 8px; }
  .preview-action { margin-top: 16px; }
  .redirect-block { margin-top: 24px; }
  .field-label { display: block; margin-bottom: 8px; font: 11px var(--bb-font-mono); color: var(--bb-tan); }
  .redirect { border-block: 1px solid var(--bb-border); padding: 12px 0; display: flex; gap: 8px; align-items: center; }
  code { font: 11px/1.6 var(--bb-font-mono); color: var(--bb-tan-pale); overflow-wrap: anywhere; min-width: 0; }
  .redirect code { flex: 1; user-select: all; }
  .hint { margin: 0; font: 12px/1.6 var(--bb-font-body); color: var(--bb-muted); }
  .actions + .hint { margin-top: 16px; }
  .saved { font: 13px/1.5 var(--bb-font-body); color: var(--bb-green-glow); }
  .error { font: 13px/1.5 var(--bb-font-body); color: var(--bb-danger); }
  .small { font-size: 13px; }
  footer { padding: 20px var(--setup-gutter) 28px; display: flex; flex-wrap: wrap; justify-content: space-between; gap: 16px; font: 11px var(--bb-font-mono); color: var(--bb-muted); }
  footer a { color: var(--bb-muted); text-decoration: none; }
  footer a:hover, .back:hover { color: var(--bb-tan-pale); }
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
