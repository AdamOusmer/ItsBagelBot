<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/error-scene.css';
  import type { Snippet } from 'svelte';
  import LightField from './LightField.svelte';

  let {
    status,
    eyebrow,
    title,
    description,
    aside,
    class: className = '',
    labelledBy = 'bb-error-title',
    actions,
    ...rest
  }: {
    status: number | string;
    eyebrow: string;
    title: string;
    description: string;
    aside?: string;
    class?: string;
    labelledBy?: string;
    actions?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-error-scene', className || null].filter(Boolean).join(' '));
</script>

<main class={classes} aria-labelledby={labelledBy} {...rest}>
  <LightField />
  <div class="bb-error-scene__glow" aria-hidden="true"></div>

  <div class="bb-error-scene__orbits" aria-hidden="true">
    <span class="bb-error-scene__orbit"></span>
    <span class="bb-error-scene__orbit bb-error-scene__orbit--two"></span>
  </div>

  <div class="bb-error-scene__content">
    <p class="bb-error-scene__eyebrow"><span>{status}</span> · {eyebrow}</p>
    <p class="bb-error-scene__code" aria-hidden="true">{status}</p>
    <h1 class="bb-error-scene__title" id={labelledBy}>{title}</h1>
    <p class="bb-error-scene__desc">{description}</p>

    {#if actions}
      <div class="bb-error-scene__actions">{@render actions()}</div>
    {/if}

    {#if aside}
      <p class="bb-error-scene__aside">{aside}</p>
    {/if}
  </div>
</main>
