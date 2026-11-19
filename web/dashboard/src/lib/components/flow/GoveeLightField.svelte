<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { invalidate } from '$app/navigation';
  import { page } from '$app/state';
  import { actionPayload, getI18n, type ActionOk, type GoveeDevice } from '@bagel/kit';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Spinner from '@bagel/ui/svelte/Spinner.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { FlowNode } from '#lib/flow/model';
  import { configText } from '#lib/flow/setup';

  let keySaved = $state(false);

  const SCENE_KEYS = ['scene', 'sceneName', 'sceneType', 'sceneInstance', 'sceneValue'];

  type GoveeLights ={ keyPresent: boolean; devices: GoveeDevice[]; code?: 'key_rejected' | 'unreachable' };

  let { node, id, tag, fieldError, onConfig }: { node: FlowNode; id: string; tag?: string; fieldError?: string; onConfig: (config: FlowNode['config']) => void } = $props();

  const { t } = getI18n();
  const lights = $derived(page.data.govee as Promise<GoveeLights>);

  let saving = $state(false);
  let error = $state('');
  let notice = $state('');
  let refreshing = $state(false);

  const device = $derived(configText(node, 'device'));
  const mode = $derived(configText(node, 'mode') || 'color');

  const hasWarmth = (light: GoveeDevice) => (light.kelvin_min ?? 0) > 0;
  const controllable = (light: GoveeDevice) => light.color || light.power || light.brightness || hasWarmth(light);
  const optionsFor = (devices: GoveeDevice[]) => {
    const options = devices.map((d) => ({ value: d.device, label: d.name || d.sku }));
    if (device && !options.some((o) => o.value === device)) options.push({ value: device, label: String(node.config.deviceName || device) });
    return options;
  };

  function supportsLook(light: GoveeDevice): boolean {
    if (mode === 'white') return hasWarmth(light);
    if (mode === 'on' || mode === 'off') return light.power ?? light.color;
    return light.color;
  }

  function lookMismatch(light: GoveeDevice | undefined): 'white' | 'color' | undefined {
    if (!light || supportsLook(light)) return undefined;
    if (mode === 'white') return 'white';
    if (mode === 'color' || mode === 'scene') return 'color';
  }

  function choose(devices: GoveeDevice[], id: string) {
    const light = devices.find((d) => d.device === id);
    if (!light) return;
    const config = { ...node.config };
    if (node.config.device !== light.device) {
      for (const key of SCENE_KEYS) delete config[key];
    }
    const next = [...extras].filter((ref) => ref.split('/')[0] !== light.device);
    onConfig(withExtras({ ...config, device: light.device, sku: light.sku, deviceName: light.name || light.sku }, next));
  }

  const refOf = (d: GoveeDevice) => `${d.device}/${d.sku}`;
  const extras = $derived(new Set(configText(node, 'extraLights').split(',').filter(Boolean)));

  function withExtras(config: FlowNode['config'], refs: string[]) {
    if (refs.length) config.extraLights = refs.join(',');
    else delete config.extraLights;
    return config;
  }

  function toggleExtra(d: GoveeDevice, on: boolean) {
    const next = new Set(extras);
    if (on) next.add(refOf(d));
    else next.delete(refOf(d));
    onConfig(withExtras({ ...node.config }, [...next]));
  }

  async function refresh() {
    refreshing = true;
    try {
      await invalidate('flows:govee');
    } finally {
      refreshing = false;
    }
  }

  const submit: SubmitFunction = () => {
    saving = true;
    error = '';
    notice = '';
    return async ({ result }) => {
      saving = false;
      const payload = actionPayload<ActionOk & { code?: string }>(result);
      if (result.type !== 'success' || payload?.ok === false) {
        error = payload?.code === 'key_invalid' ? t('govee.keyInvalid') : (payload?.error ?? t('govee.keySaveFailed'));
        return;
      }
      if (payload?.code === 'key_unchecked') notice = t('govee.keyUnchecked');
      keySaved = true;
      await invalidate('flows:govee');
    };
  };
</script>

{#await lights}
  <div class="row"><Spinner /><Text size="sm" tone="muted">{t('flows.lightField')}</Text></div>
{:then { keyPresent, devices, code }}
  {#if (!keyPresent && !keySaved) || code === 'key_rejected'}
    <form class="key" method="POST" action="/govee?/saveKey" use:enhance={submit}>
      {#if code === 'key_rejected'}
        <AlertBanner tone="warning">{t('goveeSetup.devicesKeyRejected')}</AlertBanner>
      {:else}
        <Text size="sm">{t('flows.lightKeyBody')}</Text>
      {/if}
      <Text size="sm" tone="muted">{t('flows.lightKeyPath', { path: t('govee.keyPath') })}</Text>
      <Field fit label={t('govee.keyFieldLabel')} for="{id}-key">
        <Input id="{id}-key" fill mono name="key" type="password" placeholder={t('govee.keyPlaceholder')} autocomplete="off" spellcheck="false" required />
      </Field>
      <Text size="xs" tone="muted">{t('goveeSetup.keyStored')}</Text>
      {#if error}<AlertBanner tone="danger">{error}</AlertBanner>{/if}
      <Button type="submit" busy={saving}>{t('flows.lightKeySave')}</Button>
    </form>
  {:else}
    {@const lights = devices.filter(controllable)}
    {@const options = optionsFor(lights)}
    {#if notice}<AlertBanner tone="warning">{notice}</AlertBanner>{/if}
    {#if options.length}
      {@const mismatch = lookMismatch(devices.find((light) => light.device === device))}
      {@const hints = [
        { look: 'color', text: t('flows.step.reply.lights.noColors') },
        { look: 'white', text: t('flows.step.reply.lights.noWarmth') }
      ]}
      {@const others = lights.filter((light) => light.device !== device)}
      <Field fit label={t('flows.lightField')} {tag} error={fieldError} errorId="{id}-error" for={id}>
        <Select
          {id}
          {options}
          aria-invalid={Boolean(fieldError)}
          aria-describedby={[fieldError && `${id}-error`, mismatch && `${id}-look`].filter(Boolean).join(' ') || undefined}
          value={device}
          placeholder={t('flows.lightChoose')}
          onchange={(e) => choose(lights, e.currentTarget.value)}
        />
      </Field>
      <div id="{id}-look" class="look-hint">
        {#each hints as hint (hint.look)}
          {@const hidden = hint.look !== mismatch}
          <span class="look-hint-text bb-fit-box" class:faded={hidden} aria-hidden={hidden}>
            <Text as="small" size="xs" tone="muted"><bb-fit mode="block">{hint.text}</bb-fit></Text>
          </span>
        {/each}
      </div>
      {#if device && others.length}
        <fieldset class="more">
          <Label as="legend">{t('govee.groupLightsLabel')}</Label>
          {#each others as light (light.device)}
            {@const disabled = !supportsLook(light)}
            <Checkbox class="light-option" {disabled} bind:checked={() => extras.has(refOf(light)), (on) => toggleExtra(light, on)}>
              <Text as="span" size="sm" truncate>{light.name || light.device}</Text>
            </Checkbox>
          {/each}
        </fieldset>
      {/if}
    {/if}
    {#if code === 'unreachable'}
      <Text size="sm" tone="muted">{t('govee.devicesError')}</Text>
      <Button variant="ghost" busy={refreshing} onclick={refresh}>{t('govee.devicesRetry')}</Button>
    {:else if !options.length}
      <Text size="sm" tone="muted">{t('flows.step.reply.lights.noLights')}</Text>
      <Text size="sm" tone="muted">{t('flows.step.reply.lights.noLightsBody')}</Text>
      <Button variant="ghost" busy={refreshing} onclick={refresh}>{t('govee.noLightsCta')}</Button>
    {/if}
  {/if}
{/await}

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .look-hint {
    display: grid;
    grid-template-rows: minmax(0, 1fr);
    height: 32px;
    padding-top: 4px;
  }
  .look-hint-text {
    grid-area: 1 / 1;
    min-width: 0;
  }
  .look-hint-text,
  .more :global(.light-option) { transition: opacity 0.15s ease; }
  .faded { opacity: 0; }
  @media (prefers-reduced-motion: reduce) {
    .look-hint-text,
    .more :global(.light-option) { transition: none; }
  }
  .more {
    border: none;
    margin: 0;
    padding: 0;
    min-width: 0;
    display: grid;
    gap: 8px;
  }
  .key {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }
</style>
