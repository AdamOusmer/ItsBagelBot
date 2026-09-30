<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/overview-grid.css';
  import type { Snippet } from 'svelte';

  type Own = {
    class?: string;
    main?: Snippet;
    side?: Snippet;
  };

  let {
    class: className = '',
    main,
    side,
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const classes = $derived(['bb-ov-row', className || null].filter(Boolean).join(' '));
</script>

<div class={classes} {...rest}><div class="bb-ov-row__main">{#if main}{@render main()}{/if}</div><div
    class="bb-ov-row__side"
  >{#if side}{@render side()}{/if}</div></div>
