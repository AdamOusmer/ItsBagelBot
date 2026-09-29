<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import '../styles/tags.css';
  import '../styles/elements/radio-group.css';
  import Icon from './Icon.svelte';

  type RadioOption = { value: string; label: string; description?: string; meta?: string; disabled?: boolean };

  let {
    name,
    options,
    value = $bindable(''),
    label = 'Options',
    variant = 'tabs',
    min,
    cols,
    rail,
    maxHeight,
    onchange,
    onpick,
    lead,
    class: className = '',
    ...rest
  }: {
    name: string;
    options: readonly { value: string; label: string; description?: string; meta?: string; disabled?: boolean }[];
    value: string;
    label?: string;
    variant?: 'tabs' | 'cards' | 'rows';
    min?: string;
    cols?: number;
    rail?: 'sm' | 'md';
    maxHeight?: string;
    onchange?: (value: string) => void;
    onpick?: (value: string) => void;
    lead?: Snippet<[RadioOption, boolean]>;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const tabsClass = $derived(['bb-tabs bb-tabs--wrap', className || null].filter(Boolean).join(' '));
  const choicesClass = $derived(
    [
      'bb-choices',
      `bb-choices--${variant}`,
      cols ? 'bb-choices--cols' : null,
      rail ? `bb-choices--rail-${rail}` : null,
      maxHeight ? 'bb-choices--scroll bb-scroll' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
  const choicesStyle = $derived(
    [min && `--choices-min: ${min};`, cols && `--choices-cols: ${cols};`, maxHeight && `--choices-max-h: ${maxHeight};`]
      .filter(Boolean)
      .join(' ') || undefined,
  );

  function pick(next: string) {
    value = next;
    onchange?.(next);
    onpick?.(next);
  }

  function repick(next: string) {
    if (next === value) onpick?.(next);
  }
</script>

{#if variant === 'tabs'}
  <div class={tabsClass} role="radiogroup" aria-label={label} {...rest}>
    {#each options as opt (opt.value)}
      <label class="bb-tab {value === opt.value ? 'is-active' : ''}">
        <input
          class="bb-tab__input"
          type="radio"
          {name}
          value={opt.value}
          checked={value === opt.value}
          disabled={opt.disabled}
          onclick={() => repick(opt.value)}
          onchange={() => pick(opt.value)}
        />
        {opt.label}
      </label>
    {/each}
  </div>
{:else}
  <div class={choicesClass} role="radiogroup" aria-label={label} style={choicesStyle} {...rest}>
    {#each options as opt (opt.value)}
      <label class="bb-choice"
        ><input
          class="bb-choice__input"
          type="radio"
          {name}
          value={opt.value}
          checked={value === opt.value}
          disabled={opt.disabled}
          onclick={() => repick(opt.value)}
          onchange={() => pick(opt.value)}
        />{#if variant === 'cards'}<span class="bb-choice__top"
            >{#if lead}<span class="bb-choice__lead">{@render lead(opt, value === opt.value)}</span>{/if}<span
              class="bb-choice__tick"
              aria-hidden="true"><Icon name="check" size={12} /></span
            ></span
          >{/if}<span class="bb-choice__label">{opt.label}</span>{#if opt.description}<span class="bb-choice__desc"
            >{opt.description}</span
          >{/if}{#if opt.meta}<span class="bb-choice__meta">{opt.meta}</span>{/if}</label
      >
    {/each}
  </div>
{/if}
