<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import type { Snippet } from 'svelte';
  import '../styles/elements/toggle.css';
  import Switch from './Switch.svelte';

  type Own = {
    label: string;
    hint?: string;
    hintId?: string;
    checked?: boolean;
    switchLabel?: string;
    control?: 'start' | 'end';
    tone?: 'warn';
    disabled?: boolean;
    pending?: boolean;
    type?: 'button' | 'submit';
    onchange?: (v: boolean) => void;
    status?: Snippet;
    note?: Snippet;
    class?: string;
  };

  let {
    label,
    hint,
    hintId,
    checked = $bindable(false),
    switchLabel,
    control = 'start',
    tone,
    disabled = false,
    pending = false,
    type = 'button',
    onchange,
    status,
    note,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const classes = $derived(
    [
      'bb-switch-row',
      control === 'end' ? 'bb-switch-row--end' : null,
      tone ? `bb-switch-row--${tone}` : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
  const describedby = $derived(hint ? hintId : undefined);
</script>

{#snippet toggle()}<Switch
    bind:checked
    label={switchLabel ?? label}
    {describedby}
    {disabled}
    {pending}
    {type}
    {onchange}
  />{/snippet}

<div class={classes} {...rest}
  >{#if control === 'start'}{@render toggle()}{/if}<span class="bb-switch-row__text"
    ><span class="bb-switch-row__label">{label}</span>{#if hint}<span class="bb-switch-row__hint" id={hintId}
        >{hint}</span
      >{/if}</span
  >{#if status}<span class="bb-switch-row__status">{@render status()}</span>{/if}{#if control === 'end'}{@render
      toggle()}{/if}{#if note}<span class="bb-switch-row__note">{@render note()}</span>{/if}</div
>
