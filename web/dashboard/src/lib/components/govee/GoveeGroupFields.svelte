<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import FieldError from '@bagel/ui/svelte/FieldError.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { GOVEE_GROUP_MAX_LIGHTS, GOVEE_GROUP_MIN_LIGHTS, GOVEE_GROUP_NAME_MAX, getI18n, type GoveeLight } from '@bagel/kit';
  import { goveeGroupErrors, type GoveeDraft } from './govee-draft';

  let {
    draft = $bindable<GoveeDraft>(),
    options,
    attempted = false
  }: {
    draft: GoveeDraft;
    options: GoveeLight[];
    attempted?: boolean;
  } = $props();

  const { t } = getI18n();

  const failed = $derived(goveeGroupErrors(draft));
  const nameError = $derived(attempted && failed.name ? t('govee.errGroupName') : undefined);
  const lightsError = $derived(
    attempted && failed.lights ? t('govee.errGroupLights', { min: GOVEE_GROUP_MIN_LIGHTS, max: GOVEE_GROUP_MAX_LIGHTS }) : undefined
  );
  const chosen = $derived(new Set(draft.lights.map((l) => l.device)));

  function toggle(light: GoveeLight, on: boolean) {
    draft.lights = options.filter((o) => (o.device === light.device ? on : chosen.has(o.device)));
  }
</script>

<Field label={t('govee.groupNameLabel')} error={nameError} errorId="govee-group-name-err">
  <Input
    fill
    invalid={!!nameError}
    type="text"
    maxlength={GOVEE_GROUP_NAME_MAX}
    bind:value={draft.name}
    aria-invalid={nameError ? 'true' : undefined}
    aria-describedby={nameError ? 'govee-group-name-err' : undefined}
    required
  />
</Field>

<fieldset class="lights" aria-describedby="govee-group-lights-hint">
  <Label as="legend">{t('govee.groupLightsLabel')}</Label>
  <Text as="span" size="xs" tone="muted" id="govee-group-lights-hint">{t('govee.groupLightsHint')}</Text>
  <div class="checks">
    {#each options as light, i (light.device)}
      <Checkbox
        bind:checked={() => chosen.has(light.device), (on) => toggle(light, on)}
        aria-invalid={lightsError && i === 0 ? 'true' : undefined}
      >
        <Text as="span" size="sm" truncate>{light.name || light.device}</Text>
      </Checkbox>
    {/each}
  </div>
  {#if lightsError}<FieldError message={lightsError} />{/if}
</fieldset>

<style>
  .lights {
    border: none;
    margin: 0;
    padding: 0;
    min-width: 0;
    display: grid;
    gap: 8px;
  }
  .checks {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
</style>
