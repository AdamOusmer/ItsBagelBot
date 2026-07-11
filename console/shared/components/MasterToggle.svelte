<script lang="ts">
  // Server-form "master enable" bar: a POST form whose submit button is worn as
  // a switch, plus a label/hint column. Replaces the copy-pasted
  // <form ?/toggle> + button.toggle + master-text block on timers/loyalty/etc.
  // Optimistic: flips `enabled` on submit, reverts + toasts on a non-success.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { toast } from '../lib/toast';
  import Switch from './Switch.svelte';

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

<form method="POST" {action} use:enhance={submit} class="master">
  <input type="hidden" {name} value={enabled ? '' : 'on'} />
  <Switch type="submit" checked={enabled} label={ariaLabel ?? label} describedby={hint ? hintId : undefined} />
  <span class="txt">
    <span class="lbl">{label}</span>
    {#if hint}<span class="hint" id={hintId}>{hint}</span>{/if}
  </span>
</form>

<style>
  .master { display: inline-flex; align-items: center; gap: 12px; }

  .txt { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
  .lbl {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 13px;
    color: var(--bb-white);
  }
  .hint { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-muted); }
</style>
