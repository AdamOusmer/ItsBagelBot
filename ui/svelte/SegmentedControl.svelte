<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import { getUiI18n } from './i18n';
  import '../styles/tags.css';
  import { rovingTarget } from '../lib/roving-focus';

  const i18n = getUiI18n();
  type Own = {
    options: readonly (string | { value: string; label: string; count?: number | string; attrs?: Record<string, string> })[];
    value: string;
    label?: string;
    onchange?: (value: string) => void;
    class?: string;
  };

  let {
    options,
    value = $bindable(''),
    label = i18n.t('choice.filter'),
    onchange,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const RADIOS = { selector: 'button[role="radio"]', orientation: 'both', wrap: true } as const;

  const items = $derived(options.map((opt) => (typeof opt === 'string' ? { value: opt, label: opt } : opt)));
  const classes = $derived(['bb-tabs bb-tabs--wrap', className || null].filter(Boolean).join(' '));

  function pick(next: string) {
    value = next;
    onchange?.(next);
  }

  function onkeydown(event: KeyboardEvent) {
    const next = rovingTarget(event.currentTarget as HTMLElement, event, RADIOS);
    if (!next) return;
    event.preventDefault();
    next.focus();
    next.click();
  }
</script>

<div class={classes} role="radiogroup" aria-label={label} {onkeydown} {...rest}>
  {#each items as opt (opt.value)}
    <button
      type="button"
      class="bb-tab {value === opt.value ? 'is-active' : ''}"
      role="radio"
      aria-checked={value === opt.value}
      value={opt.value}
      onclick={() => pick(opt.value)}
      {...opt.attrs}
    >
      {opt.label}{#if opt.count !== undefined}<span class="bb-tab__count">{opt.count}</span>{/if}
    </button>
  {/each}
</div>
