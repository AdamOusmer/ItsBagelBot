<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/error-scene.css';
  import type { Snippet } from 'svelte';
  import LightField from './LightField.svelte';

  type ErrorSceneAction = {
    label: string;
    href?: string;
    onclick?: (event: MouseEvent) => void;
    [attribute: string]: unknown;
  };

  type Own = {
    status: number | string;
    eyebrow: string;
    title: string;
    description: string;
    aside?: string;
    class?: string;
    labelledBy?: string;
    primary?: ErrorSceneAction;
    secondary?: ErrorSceneAction;
    actions?: Snippet;
  };

  let {
    status,
    eyebrow,
    title,
    description,
    aside,
    class: className = '',
    labelledBy = 'bb-error-title',
    primary,
    secondary,
    actions,
    ...rest
  }: Own & Omit<SvelteHTMLElements['main'], keyof Own> = $props();

  const classes = $derived(['bb-error-scene', className || null].filter(Boolean).join(' '));
  const hasActions = $derived(Boolean(primary || secondary || actions));
</script>

{#snippet control(action: ErrorSceneAction, tone: 'primary' | 'quiet')}
  {@const { label, href, ...attributes } = action}
  {#if href}<a class="bb-error-scene__action bb-error-scene__action--{tone}" {href} {...attributes}>{label}</a
    >{:else}<button class="bb-error-scene__action bb-error-scene__action--{tone}" type="button" {...attributes}
      >{label}</button
    >{/if}
{/snippet}

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

    {#if hasActions}
      <div class="bb-error-scene__actions"
        >{#if primary}{@render control(primary, 'primary')}{/if}{#if secondary}{@render control(
            secondary,
            'quiet',
          )}{/if}{#if actions}{@render actions()}{/if}</div
      >
    {/if}

    {#if aside}
      <p class="bb-error-scene__aside">{aside}</p>
    {/if}
  </div>
</main>
