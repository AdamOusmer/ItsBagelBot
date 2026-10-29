<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { tick } from 'svelte';
  import { dev } from '$app/env';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { actionPayload, type ActionOk, type GoveeDevice } from '@bagel/kit';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Spinner from '@bagel/ui/svelte/Spinner.svelte';
  import SetupStage from '@bagel/ui/svelte/SetupStage.svelte';
  import SetupStep from '@bagel/ui/svelte/SetupStep.svelte';
  import SetupNote from '@bagel/ui/svelte/SetupNote.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
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

<SetupStage
  label={t('goveeSetup.regionLabel')}
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
  {#snippet start()}<TextLink variant="quiet" icon="arrowLeft" href="/modules" label={t('govee.back')} />{/snippet}
  {#snippet art()}
    <Bolota {name} size={240} active follow cycle={false}
      expression={done ? 'proud' : hover ? 'excited' : faces[step]}
      sequence={done ? 'burst' : 'entrance'} {sequenceKey} sequenceFor={1100} sequenceHold={240} />
  {/snippet}
  {#snippet aside()}
    {#if !done}<SetupAskAi feature="govee" step={railSteps[step].label} stepIndex={step} error={aiError} />{/if}
  {/snippet}
  {#snippet foot()}
    <Text as="span" size="xs" mono tone="muted">{dev && preview ? t('goveeSetup.preview') : t('goveeSetup.footer')}</Text>
  {/snippet}
  {#snippet end()}<TextLink variant="quiet" href="/modules" label={`${t('goveeSetup.later')} ↗`} />{/snippet}
  {#key `${step}-${done}`}
    <SetupStep
      kicker={`${t('goveeSetup.eyebrow')} / ${String(step + 1).padStart(2, '0')}`}
      title={t(`goveeSetup.${titles[step]}`)}
      body={t(`goveeSetup.${bodies[step]}`)}
      {dir}
      {hero}
      bind:heading
    >
      {#if step === 0}
        <SetupNote title={t('goveeSetup.needNoteTitle')} body={t('goveeSetup.needNoteBody')}>
          {#snippet icon()}<Icon name="bulb" size={18} strokeWidth={1.7} />{/snippet}
        </SetupNote>
        <div class="bb-setup-actions"><Button onclick={() => go(1)} onpointerenter={() => hover = true} onpointerleave={() => hover = false}>{t('goveeSetup.start')} →</Button></div>
      {:else if step === 1}
        <div class="block">
          <Label mono as="span">{t('goveeSetup.pathLabel')}</Label>
          <div class="bb-setup-row"><Text as="span" size="sm" tone="pale">{#each keyPath as part, i}{#if i > 0}<span class="path-arrow" aria-hidden="true">→</span>{/if}<strong>{part}</strong>{/each}</Text></div>
          <Text size="xs" tone="muted">{t('goveeSetup.keyEmailHint')}</Text>
          <Text size="xs" tone="muted">{t('goveeSetup.keyOlderHint')}</Text>
        </div>
        <div class="bb-setup-actions"><Button variant="ghost" onclick={() => go(0)}>{t('onboarding.back')}</Button><Button onclick={() => go(2)}>{t('goveeSetup.haveKey')} →</Button></div>
      {:else if step === 2}
        <form method="POST" action="?/saveKey" use:enhance={submit}>
          <Field label={t('govee.keyFieldLabel')}><Input fill mono name="key" type="password" placeholder={t('govee.keyPlaceholder')} autocomplete="off" spellcheck="false" required /></Field>
          <Text size="xs" tone="muted">{t('goveeSetup.keyStored')}</Text>
          <div class="bb-setup-actions"><Button variant="ghost" onclick={() => go(1)} disabled={saving}>{t('onboarding.back')}</Button><Button type="submit" busy={saving}>{t('goveeSetup.saveContinue')} →</Button></div>
        </form>
        {#if dev && preview}<div class="bb-setup-actions"><Button variant="ghost" onclick={usePreviewKey}>{t('goveeSetup.previewKey')} →</Button></div>{/if}
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
        <div class="bb-setup-actions">
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
      {#snippet status()}
        {#if message}<AlertBanner tone={saveError ? 'danger' : 'warning'} flush>{message}</AlertBanner>{/if}
      {/snippet}
    </SetupStep>
  {/key}
</SetupStage>

<style>
  .block { display: grid; gap: 8px; }
  .path-arrow { color: var(--bb-muted); margin-inline: 10px; }
  form { --field-mb: 0; display: grid; gap: 14px; }
  .lights-found { display: grid; gap: 8px; min-height: 96px; padding: 16px 18px; background: var(--bb-elev-1); border-radius: var(--bb-radius-md); box-shadow: var(--bb-depth-1); }
  .state { display: flex; align-items: center; gap: 10px; }
  .lights-found ul { margin: 0; padding-left: 18px; display: grid; gap: 4px; }
</style>
