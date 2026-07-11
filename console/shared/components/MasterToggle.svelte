<script lang="ts">
  // Server-form "master enable" bar: a POST form whose submit button is worn as
  // a switch, plus a label/hint column. Replaces the copy-pasted
  // <form ?/toggle> + button.toggle + master-text block on timers/loyalty/etc.
  // Optimistic: flips `enabled` on submit, reverts + toasts on a non-success.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { toast } from '../lib/toast';

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
  <button class="toggle {enabled ? 'on' : ''}" type="submit" aria-label={ariaLabel ?? label}></button>
  <span class="txt">
    <span class="lbl">{label}</span>
    {#if hint}<span class="hint">{hint}</span>{/if}
  </span>
</form>

<style>
  .master { display: inline-flex; align-items: center; gap: 12px; }

  /* switch — copied verbatim from Toggle.svelte so the pill reads identically */
  .toggle { width: 38px; height: 22px; display: inline-block; border-radius: 999px; background: rgba(255,255,255,0.06); border: 1px solid var(--glass-border); position: relative; cursor: pointer; flex-shrink: 0;
    transition: all var(--bb-dur-base) var(--bb-ease-out-back); }
  .toggle::after { content: ""; position: absolute; top: 2px; left: 2px; width: 16px; height: 16px; border-radius: 50%; background: var(--bb-muted);
    transition: all var(--bb-dur-base) var(--bb-ease-out-back); }
  .toggle.on { background: var(--ui-accent-soft); border-color: rgba(82,183,136,0.4); }
  .toggle.on::after { left: 18px; background: var(--bb-green-glow); box-shadow: 0 0 8px var(--bb-green-glow); }

  .txt { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
  .lbl {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 13px;
    color: var(--bb-white);
  }
  .hint { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-muted); }
</style>
