<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { toast } from '@bagel/ui/svelte/toast';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import { getI18n } from '../lib/i18n/context';

  const { t } = getI18n();

  let {
    action,
    enabled = $bindable(),
    label,
    hint,
    name = 'is_enabled',
    ariaLabel,
    failMessage = t('serverErrors.updateRetry')
  }: {
    action: string;
    enabled: boolean;
    label: string;
    hint?: string;
    name?: string;
    ariaLabel?: string;
    failMessage?: string;
  } = $props();

  const uid = $props.id();
  const hintId = `master-hint-${uid}`;

  const submit: SubmitFunction = () => {
    const was = enabled;
    enabled = !was;
    return async ({ result }) => {
      if (result.type !== 'success') {
        enabled = was;
        toast('danger', failMessage);
      }
    };
  };
</script>

<form method="POST" {action} use:enhance={submit} class="master">
  <input type="hidden" {name} value={enabled ? '' : 'on'} />
  <div class="layer" class:shown={!enabled} inert={enabled}>
    <AlertBanner tone="warning" role="status" flush strong>
      {t('modules.offBanner')}
      {#snippet actions()}<Button type="submit">{t('modules.turnOn')}</Button>{/snippet}
    </AlertBanner>
  </div>
  <div class="layer" class:shown={enabled} inert={!enabled}>
    <SwitchRow type="submit" checked={enabled} {label} {hint} {hintId} switchLabel={ariaLabel ?? label} />
  </div>
</form>

<style>
  .master { display: grid; min-width: 0; }
  .layer {
    grid-area: 1 / 1;
    align-self: center;
    min-width: 0;
    opacity: 0;
    visibility: hidden;
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo), visibility 0s linear var(--bb-dur-base);
  }
  .layer.shown {
    opacity: 1;
    visibility: visible;
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo), visibility 0s;
  }
</style>
