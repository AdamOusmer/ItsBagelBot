<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { invalidate } from '$app/navigation';
  import { page } from '$app/state';
  import { actionPayload, getI18n, type ActionOk, type GoveeDevice } from '@bagel/kit';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Spinner from '@bagel/ui/svelte/Spinner.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { FlowNode } from '#lib/flow/model';
  import { configText } from '#lib/flow/setup';

  let keySaved = $state(false);

  const SCENE_KEYS = ['scene', 'sceneName', 'sceneType', 'sceneInstance', 'sceneValue'];

  type GoveeLights ={ keyPresent: boolean; devices: GoveeDevice[]; code?: 'key_rejected' | 'unreachable' };

  let { node, id }: { node: FlowNode; id: string } = $props();

  const { t } = getI18n();
  const lights = $derived(page.data.govee as Promise<GoveeLights>);

  let saving = $state(false);
  let error = $state('');
  let notice = $state('');
  let refreshing = $state(false);

  const device = $derived(configText(node, 'device'));

  const optionsFor = (devices: GoveeDevice[]) => {
    const options = devices.filter((d) => d.color).map((d) => ({ value: d.device, label: d.name || d.sku }));
    if (device && !options.some((o) => o.value === device)) options.push({ value: device, label: String(node.config.deviceName || device) });
    return options;
  };

  function choose(devices: GoveeDevice[], id: string) {
    const light = devices.find((d) => d.device === id);
    if (!light) return;
    if (node.config.device !== light.device) {
      for (const key of SCENE_KEYS) delete node.config[key];
    }
    node.config.device = light.device;
    node.config.sku = light.sku;
    node.config.deviceName = light.name || light.sku;
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
      <Field label={t('govee.keyFieldLabel')} for="{id}-key">
        <Input id="{id}-key" fill mono name="key" type="password" placeholder={t('govee.keyPlaceholder')} autocomplete="off" spellcheck="false" required />
      </Field>
      <Text size="xs" tone="muted">{t('goveeSetup.keyStored')}</Text>
      {#if error}<AlertBanner tone="danger">{error}</AlertBanner>{/if}
      <Button type="submit" busy={saving}>{t('flows.lightKeySave')}</Button>
    </form>
  {:else}
    {#if notice}<AlertBanner tone="warning">{notice}</AlertBanner>{/if}
    {@const options = optionsFor(devices)}
    {#if options.length}
      <Field label={t('flows.lightField')} for={id}>
        <Select
          {id}
          {options}
          value={device}
          placeholder={t('flows.lightChoose')}
          onchange={(e) => choose(devices, e.currentTarget.value)}
        />
      </Field>
    {/if}
    {#if code === 'unreachable'}
      <Text size="sm" tone="muted">{t('govee.devicesError')}</Text>
      <Button variant="ghost" busy={refreshing} onclick={refresh}>{t('govee.devicesRetry')}</Button>
    {:else if !options.length}
      <Text size="sm" tone="muted">{t('govee.noLights')}</Text>
      <Text size="sm" tone="muted">{t('govee.noLightsBody')}</Text>
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
  .key {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }
</style>
