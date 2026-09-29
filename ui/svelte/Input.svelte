<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/field.css';
  import '../styles/elements/input.css';
  import type { Snippet } from 'svelte';

  type Own = {
    value?: string | number | null;
    type?: 'text' | 'email' | 'url' | 'tel' | 'number' | 'password' | 'search' | 'date' | 'datetime-local' | 'month' | 'time' | 'week' | 'color';
    invalid?: boolean;
    fill?: boolean;
    mono?: boolean;
    align?: 'start' | 'end';
    class?: string;
    icon?: Snippet;
    trail?: Snippet;
  };

  let {
    value = $bindable(''),
    type = 'text',
    invalid = false,
    fill = false,
    mono = false,
    align = 'start',
    class: className = '',
    icon,
    trail,
    ...rest
  }: Own & Omit<SvelteHTMLElements['input'], keyof Own> = $props();

  const classes = $derived(
    [
      'bb-input',
      type === 'color' ? 'bb-input--color' : null,
      fill ? 'bb-input--fill' : null,
      mono ? 'bb-input--mono' : null,
      align === 'end' ? 'bb-input--end' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<span class={classes} data-invalid={invalid ? '' : undefined}>{#if icon}{@render icon()}{/if}<input
    {type}
    bind:value
    {...rest}
  />{#if trail}{@render trail()}{/if}</span>
