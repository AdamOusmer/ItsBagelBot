<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { toast } from '@bagel/ui/svelte/toast';
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
    failMessage = t('serverErrors.updateFailed')
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
        toast('err', failMessage);
      }
    };
  };
</script>

<form method="POST" {action} use:enhance={submit}>
  <input type="hidden" {name} value={enabled ? '' : 'on'} />
  <SwitchRow type="submit" checked={enabled} {label} {hint} {hintId} switchLabel={ariaLabel ?? label} />
</form>
