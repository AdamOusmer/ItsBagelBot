<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import type { Snippet } from 'svelte';
  import '../styles/elements/picker-panel.css';
  import Icon from './Icon.svelte';

  type Own = {
    label?: string;
    description?: string;
    layout?: 'inline' | 'stacked';
    selected?: boolean;
    disabled?: boolean;
    onclick?: () => void;
    remove?: { label: string; armed?: boolean; armedLabel?: string; onclick: () => void };
    as?: 'div' | 'li';
    class?: string;
    children?: Snippet;
    trail?: Snippet;
  };

  let {
    label,
    description,
    layout = 'inline',
    selected = false,
    disabled = false,
    onclick,
    remove,
    as: tag = 'div',
    class: className = '',
    children,
    trail,
    ...rest
  }: Own & Omit<SvelteHTMLElements['button'], keyof Own> = $props();

  const classes = $derived(
    ['bb-picker-option', layout === 'stacked' ? 'bb-picker-option--stacked' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes}
  ><button
    type="button"
    class="bb-picker-option__main"
    aria-current={selected ? 'true' : undefined}
    {disabled}
    {onclick}
    {...rest}
    >{#if children}{@render children()}{:else}<span class="bb-picker-option__label">{label}</span>{/if}{#if description}<span
        class="bb-picker-option__desc">{description}</span
      >{/if}{#if trail}{@render trail()}{/if}</button
  >{#if remove}<button
      type="button"
      class="bb-picker-option__remove"
      aria-label={remove.label}
      data-armed={remove.armed ? '' : undefined}
      onclick={remove.onclick}
      >{#if remove.armed}{remove.armedLabel}{:else}<Icon name="x" size={12} />{/if}</button
    >{/if}</svelte:element
>
