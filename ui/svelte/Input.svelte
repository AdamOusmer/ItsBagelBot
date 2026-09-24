<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/field.css';
  import '../styles/elements/input.css';
  import type { Snippet } from 'svelte';

  let {
    value = $bindable(''),
    type = 'text',
    invalid = false,
    fill = false,
    mono = false,
    class: className = '',
    icon,
    trail,
    ...rest
  }: {
    value?: string | number | null;
    type?: 'text' | 'email' | 'url' | 'tel' | 'number' | 'password' | 'search' | 'date' | 'datetime-local' | 'month' | 'time' | 'week';
    invalid?: boolean;
    fill?: boolean;
    mono?: boolean;
    class?: string;
    icon?: Snippet;
    trail?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-input', fill ? 'bb-input--fill' : null, mono ? 'bb-input--mono' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<span class={classes} data-invalid={invalid ? '' : undefined}>{#if icon}{@render icon()}{/if}<input
    {type}
    bind:value
    {...rest}
  />{#if trail}{@render trail()}{/if}</span>
