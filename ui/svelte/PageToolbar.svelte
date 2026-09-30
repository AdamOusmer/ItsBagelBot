<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/page.css';
  import type { Snippet } from 'svelte';

  type Own = {
    leading?: Snippet;
    trailing?: Snippet;
    class?: string;
  };

  let {
    leading,
    trailing,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const classes = $derived(['bb-toolbar', className || null].filter(Boolean).join(' '));
</script>

<div class={classes} {...rest}
  >{#if leading}{@render leading()}{/if}<div class="bb-toolbar__grow"></div>{#if trailing}{@render trailing()}{/if}</div
>
