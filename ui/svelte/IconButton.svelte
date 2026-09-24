<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/button.css';
  import '../styles/elements/tooltip.css';
  import type { Snippet } from 'svelte';

  let {
    label,
    tooltip = false,
    size = 'md',
    type = 'button',
    onclick,
    disabled = false,
    class: className = '',
    children,
    ...rest
  }: {
    label: string;
    tooltip?: boolean;
    size?: 'md' | 'sm';
    type?: 'button' | 'submit' | 'reset';
    onclick?: (e: MouseEvent) => void;
    disabled?: boolean;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-btn', 'bb-btn--icon', size === 'sm' ? 'bb-btn--sm' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

{#if tooltip}<span class="bb-tooltip"><button
      class={classes}
      {type}
      {disabled}
      aria-label={label}
      data-mark=""
      {onclick}
      {...rest}><span class="bb-btn__content">{@render children()}</span></button
    ><span class="bb-tooltip__bubble" aria-hidden="true">{label}</span></span
  >{:else}<button
    class={classes}
    {type}
    {disabled}
    aria-label={label}
    data-mark=""
    {onclick}
    {...rest}><span class="bb-btn__content">{@render children()}</span></button
  >{/if}
