<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/input.css';
  import type { Snippet } from 'svelte';

  type Own = {
    checked?: boolean;
    class?: string;
    children?: Snippet;
  };

  let {
    checked = $bindable(false),
    class: className = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements['input'], keyof Own> = $props();

  const classes = $derived(['bb-check', className || null].filter(Boolean).join(' '));
</script>

<label class={classes}><input type="checkbox" class="bb-check__input" bind:checked {...rest} /><span
    class="bb-check__box"
    aria-hidden="true"
  ></span>{#if children}<span class="bb-check__label">{@render children()}</span>{/if}</label>
