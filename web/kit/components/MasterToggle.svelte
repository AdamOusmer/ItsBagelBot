<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Server-form "master enable" bar: a POST form whose submit button is worn as
  // a switch, plus a label/hint column. Replaces the copy-pasted
  // <form ?/toggle> + button.toggle + master-text block on timers/loyalty/etc.
  // Optimistic: flips `enabled` on submit, reverts + toasts on a non-success.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { toast } from '@bagel/ui/svelte/toast';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  // No <style> here on purpose. The row, its label and its hint are
  // `.bb-switch-row` in @bagel/ui/styles/elements/toggle.css, next to the pill
  // they sit beside; what is left in this file is the POST form, the optimistic
  // flip and the failure toast, which is the data half.
  import '@bagel/ui/styles/elements/toggle.css';

  let {
    action,
    enabled = $bindable(),
    label,
    hint,
    name = 'is_enabled',
    ariaLabel,
    failMessage
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
        toast('err', failMessage ?? 'Could not update.');
      }
    };
  };
</script>

<form method="POST" {action} use:enhance={submit} class="bb-switch-row">
  <input type="hidden" {name} value={enabled ? '' : 'on'} />
  <Switch type="submit" checked={enabled} label={ariaLabel ?? label} describedby={hint ? hintId : undefined} />
  <span class="bb-switch-row__text">
    <span class="bb-switch-row__label">{label}</span>
    {#if hint}<span class="bb-switch-row__hint" id={hintId}>{hint}</span>{/if}
  </span>
</form>
