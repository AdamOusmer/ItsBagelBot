<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/overview-grid.css';
  import type { Snippet } from 'svelte';

  let {
    class: className = '',
    main,
    side,
    ...rest
  }: {
    class?: string;
    main?: Snippet;
    side?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-ov-row', className || null].filter(Boolean).join(' '));
</script>

<div class={classes} {...rest}><div class="bb-ov-row__main">{#if main}{@render main()}{/if}</div><div
    class="bb-ov-row__side"
  >{#if side}{@render side()}{/if}</div></div>
