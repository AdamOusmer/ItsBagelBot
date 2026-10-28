<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import type { SvelteHTMLElements } from 'svelte/elements';
  import { riseLines } from '../lib/line-rise';
  import '../styles/elements/page-hero.css';
  import LightField from './LightField.svelte';

  type Own = {
    eyebrow?: string;
    title: string;
    description?: string;
    class?: string;
  };

  let {
    eyebrow,
    title,
    description,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['header'], keyof Own> = $props();

  const classes = $derived(['bb-page-hero', className || null].filter(Boolean).join(' '));

  let heading: HTMLHeadingElement;

  onMount(() => riseLines(heading));
</script>

<header class={classes} {...rest}><LightField /><div
    class="bb-page-hero__glow"
    aria-hidden="true"
  ></div><div class="bb-page-hero__inner"
  >{#if eyebrow}<span class="bb-page-hero__eyebrow"><span data-fit="">{eyebrow}</span></span>{/if}<h1
      class="bb-page-hero__title" bind:this={heading}><span data-fit="block">{title}</span></h1
    >{#if description}<p class="bb-page-hero__desc"><span data-fit="block">{description}</span></p>{/if}</div
  ></header>
