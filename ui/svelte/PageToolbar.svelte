<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // A horizontal control row for a page header area: a lead slot, a flexible
  // spacer, a trail slot. Svelte only -- no static surface renders one yet.
  //
  // Self-contained rather than leaning on a global `.toolbar`: it used to
  // duplicate one, and the two drifted by 6px of bottom margin depending on
  // which page you were looking at. The global is deleted with this move.
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
