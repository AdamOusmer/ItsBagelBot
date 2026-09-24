<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/shell.css';
  import type { Snippet } from 'svelte';

  let {
    lead,
    trail,
    class: className = '',
    ...rest
  }: {
    lead?: Snippet;
    trail?: Snippet;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-toolbar', className || null].filter(Boolean).join(' '));
</script>

<div class={classes} {...rest}
  >{#if lead}{@render lead()}{/if}<div class="bb-toolbar__grow"></div>{#if trail}{@render trail()}{/if}</div
>
