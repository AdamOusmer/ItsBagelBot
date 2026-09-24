<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/input.css';
  import type { Snippet } from 'svelte';

  let {
    checked = $bindable(false),
    class: className = '',
    children,
    ...rest
  }: {
    checked?: boolean;
    class?: string;
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-check', className || null].filter(Boolean).join(' '));
</script>

<label class={classes}><input type="checkbox" class="bb-check__input" bind:checked {...rest} /><span
    class="bb-check__box"
    aria-hidden="true"
  ></span>{#if children}<span class="bb-check__label">{@render children()}</span>{/if}</label>
