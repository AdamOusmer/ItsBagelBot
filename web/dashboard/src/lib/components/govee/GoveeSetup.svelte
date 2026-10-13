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
  import { actionPayload, type ActionOk, type GoveeDevice } from '@bagel/kit';
  import { bezier } from '@bagel/ui/lib/tween';
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Spinner from '@bagel/ui/svelte/Spinner.svelte';
  import SetupStage from '$lib/components/shared/SetupStage.svelte';
  import SetupAskAi from '$lib/components/shared/SetupAskAi.svelte';

  type DeviceList = { devices: GoveeDevice[]; error?: string; code?: 'key_rejected' | 'unreachable' };
  let { name, devices, preview = false, onFinish }: {
    name: string; devices: DeviceList | Promise<DeviceList>; preview?: boolean; onFinish: () => void;
  } = $props();
  const { t } = getI18n();
  let step = $state(0);
  let furthest = $state(0);
  let saved = $state(false);
  const railSteps = $derived(['railNeed', 'railKey', 'railPaste', 'railLights'].map(key => ({ label: t(`goveeSetup.${key}`) })));
  const keyPath = $derived(t('govee.keyPath').split('→').map(part => part.trim().replace(/^["«»„“”]+|["«»„“”]+$/g, '').trim()));
  const titles = ['needTitle', 'keyTitle', 'pasteTitle', 'lightsTitle'];
  const bodies = ['needBody', 'keyBody', 'pasteBody', 'lightsBody'];
  const faces = ['happy', 'curious', 'attentive', 'excited'];
  let dir = $state(1);
  let sequenceKey = $state(0);
  let done = $state(false);
  let saving = $state(false);
  let refreshing = $state(false);
  let saveError = $state('');
  let notice = $state('');
  const message = $derived(saveError || notice);
  const aiError = $derived(message && !['govee.keyInvalid', 'govee.keySaveFailed', 'govee.keyUnchecked'].some(key => message === t(key)) ? t('setupAi.genericError') : message);
  let hover = $state(false);
  let heading = $state<HTMLHeadingElement | null>(null);
  const maxStep = $derived(saved ? 3 : Math.min(furthest + 1, 2));
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
  async function finish() {
    saved = true;
    dir = 1;
    step = 3;
    furthest = 3;
    done = true;
    sequenceKey += 1;
    await tick();
    heading?.focus({ preventScroll: true });
  }
  const submit: SubmitFunction = () => {
    saving = true;
    saveError = '';
    notice = '';
    return async ({ result }) => {
      saving = false;
      const payload = actionPayload<ActionOk & { code?: string }>(result);
      if (result.type !== 'success' || payload?.ok === false) {
        saveError = payload?.code === 'key_invalid' ? t('govee.keyInvalid') : payload?.error ?? t('govee.keySaveFailed');
        return;
      }
      if (payload?.code === 'key_unchecked') notice = t('govee.keyUnchecked');
      await invalidateAll();
      await finish();
    };
  };
  function usePreviewKey() {
    if (!dev || !preview) return;
    void finish();
  }
  async function refreshLights() {
    refreshing = true;
    try {
      await invalidateAll();
    } finally {
      refreshing = false;
    }
  }
</script>

<SetupStage label={t('goveeSetup.regionLabel')} {name} {step} {done} {railSteps} {maxStep} onSelect={go} expression={done ? 'proud' : hover ? 'excited' : faces[step]} {sequenceKey} backLabel={t('govee.back')} footer={dev && preview ? t('goveeSetup.preview') : t('goveeSetup.footer')} later={t('goveeSetup.later')}>
  {#snippet aside()}
    {#if !done}<SetupAskAi feature="govee" step={railSteps[step].label} stepIndex={step} error={aiError} />{/if}
  {/snippet}
  {#key `${step}-${done}`}
    <section class="step">
      <div class="kicker" in:arrive={{ i: 0 }} out:depart={{ i: 0 }}><Eyebrow as="p">{t('goveeSetup.eyebrow')} <span class="kicker-step"> / {String(step + 1).padStart(2, '0')}</span></Eyebrow></div>
      <h1 class="title" tabindex="-1" bind:this={heading} in:arrive={{ i: 1 }} out:depart={{ i: 1 }}>{t(`goveeSetup.${titles[step]}`)}</h1>
      <div class="body" in:arrive={{ i: 2 }} out:depart={{ i: 2 }}><Text tone="muted">{t(`goveeSetup.${bodies[step]}`)}</Text></div>
      <div class="controls" in:arrive={{ i: 3 }} out:depart={{ i: 3 }}>
        {#if step === 0}
          <div class="premium-note"><Text as="span" size="sm" tone="pale"><strong>{t('goveeSetup.needNoteTitle')}</strong></Text><Text size="xs" tone="muted">{t('goveeSetup.needNoteBody')}</Text></div>
          <div class="promise"><span class="promise-glyph" aria-hidden="true"><Icon name="bulb" size={20} strokeWidth={1.7} /></span><Text as="span" size="sm" tone="pale">Govee <i>×</i> ItsBagelBot</Text></div>
          <div class="actions"><Button onclick={() => go(1)} onpointerenter={() => hover = true} onpointerleave={() => hover = false}>{t('goveeSetup.start')} →</Button></div>
        {:else if step === 1}
          <div class="redirect-block">
            <Label mono as="span">{t('goveeSetup.pathLabel')}</Label>
            <div class="redirect"><Text as="span" size="sm" tone="pale">{#each keyPath as part, i}{#if i > 0}<span class="path-arrow" aria-hidden="true">→</span>{/if}<strong>{part}</strong>{/each}</Text></div>
            <div class="key-hints">
              <Text size="xs" tone="muted">{t('goveeSetup.keyEmailHint')}</Text>
              <Text size="xs" tone="muted">{t('goveeSetup.keyOlderHint')}</Text>
            </div>
          </div>
          <div class="actions"><Button variant="ghost" onclick={() => go(0)}>{t('onboarding.back')}</Button><Button onclick={() => go(2)}>{t('goveeSetup.haveKey')} →</Button></div>
        {:else if step === 2}
          <form method="POST" action="?/saveKey" use:enhance={submit}>
            <Field label={t('govee.keyFieldLabel')}><Input fill mono name="key" type="password" placeholder={t('govee.keyPlaceholder')} autocomplete="off" spellcheck="false" required /></Field>
            <Text size="xs" tone="muted">{t('goveeSetup.keyStored')}</Text>
            <div class="actions"><Button variant="ghost" onclick={() => go(1)} disabled={saving}>{t('onboarding.back')}</Button><Button type="submit" busy={saving}>{t('goveeSetup.saveContinue')} →</Button></div>
          </form>
          {#if dev && preview}<div class="preview-action"><Button variant="ghost" onclick={usePreviewKey}>{t('goveeSetup.previewKey')} →</Button></div>{/if}
        {:else}
          <div class="lights-found">
            {#await devices}
              <div class="state" role="status"><Spinner /><Text as="span" size="sm" tone="muted">{t('govee.loadingLights')}</Text></div>
            {:then dr}
              {@const lights = (dr.devices ?? []).filter((d) => d.color)}
              {#if dr.error}
                <Text size="sm" tone="muted">{t(dr.code === 'key_rejected' ? 'goveeSetup.devicesKeyRejected' : 'govee.devicesError')}</Text>
              {:else if (dr.devices ?? []).length === 0}
                <Text size="sm" tone="muted">{t('govee.noDevicesBody')}</Text>
              {:else if lights.length === 0}
                <Text size="sm" tone="muted">{t('govee.noLightsBody')}</Text>
              {:else}
                <Text size="sm" tone="muted">{t('goveeSetup.lightsFound', { n: lights.length })}</Text>
                <ul>{#each lights.slice(0, 3) as d (d.device)}<li><Text size="sm" truncate>{d.name || d.sku}</Text></li>{/each}</ul>
                {#if lights.length > 3}<Text size="sm" tone="muted">{t('goveeSetup.lightsMore', { n: lights.length - 3 })}</Text>{/if}
              {/if}
            {:catch}
              <Text size="sm" tone="muted">{t('govee.devicesError')}</Text>
            {/await}
          </div>
          <div class="actions">
            {#await devices then dr}
              {#if dr.error && dr.code === 'key_rejected'}<Button variant="ghost" onclick={() => { done = false; void go(2); }}>{t('goveeSetup.railPaste')}</Button>{/if}
              {#if dr.error || !(dr.devices ?? []).some((d) => d.color)}<Button variant="secondary" busy={refreshing} onclick={refreshLights}>{t('govee.noLightsCta')}</Button>{/if}
            {:catch}
              <Button variant="secondary" busy={refreshing} onclick={refreshLights}>{t('govee.noLightsCta')}</Button>
            {/await}
            {#if dev && preview}<ButtonLink href="/govee" onclick={onFinish}>{t('goveeSetup.readyCta')} →</ButtonLink>
            {:else}<Button onclick={onFinish}>{t('goveeSetup.readyCta')} →</Button>{/if}
          </div>
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
  .promise-glyph { display: inline-flex; color: var(--bb-green-glow); }
  .promise i { font-style: normal; padding: 0 8px; color: var(--bb-muted); }
  .actions { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; margin-top: var(--setup-actions-gap); }
  form { --field-mb: 0; display: grid; gap: min(16px, 1.2vw, 1.2vh); }
  form .actions { margin-top: 8px; }
  .preview-action { margin-top: min(16px, 1.2vw, 1.2vh); }
  .redirect-block { display: grid; gap: 8px; margin-top: min(24px, 1.8vw, 1.8vh); }
  .redirect { border-block: 1px solid var(--bb-border); padding: min(12px, 1vw, 1.3vh) 0; }
  .path-arrow { color: var(--bb-muted); margin-inline: 10px; }
  .key-hints { display: grid; gap: 4px; max-width: 48ch; }
  .lights-found { display: grid; gap: 8px; min-height: 120px; }
  .state { display: flex; align-items: center; gap: 10px; }
  .lights-found ul { margin: 0; padding-left: 18px; display: grid; gap: 4px; }
  @keyframes sheen { from { background-position: 120% 0; } to { background-position: -20% 0; } }
  @media (min-width: 761px) and (max-height: 860px), (max-width: 760px) {
    .body { --bb-text-md: 14px; }
    .premium-note { gap: 4px; padding: 10px 12px; margin-bottom: 8px; }
    .promise { padding: 6px 0; }
    form { --field-mb: 0; gap: 10px; }
    .preview-action { margin-top: 10px; }
    .redirect-block { margin-top: 14px; gap: 6px; }
    .redirect { padding-block: 8px; }
    .lights-found { min-height: 96px; }
  }
  @media (min-width: 761px) and (max-height: 760px), (max-width: 760px) {
    .premium-note { padding: 8px 10px; }
    .promise { padding: 4px 0; }
    .redirect-block { margin-top: 10px; }
    form { gap: 8px; }
    form .actions { margin-top: 4px; }
    .preview-action { margin-top: 8px; }
    .lights-found { min-height: 80px; }
  }
  @media (prefers-reduced-motion: reduce) { .title { animation: none; } }
</style>
