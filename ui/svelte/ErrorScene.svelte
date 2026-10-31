<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/orbs.css';
  import '../styles/elements/error-scene.css';
  import type { Snippet } from 'svelte';
  import { perchSplit } from '../lib/error-scene';
  import LightField from './LightField.svelte';

  type ErrorSceneAction = {
    label: string;
    href?: string;
    onclick?: (event: MouseEvent) => void;
    [attribute: string]: unknown;
  };

  type Own = {
    status?: number | string;
    eyebrow: string;
    title: string;
    description: string;
    aside?: string;
    class?: string;
    labelledBy?: string;
    primary?: ErrorSceneAction;
    secondary?: ErrorSceneAction;
    actions?: Snippet;
    mark?: Snippet;
    mascot?: Snippet;
    children?: Snippet;
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
    mark,
    mascot,
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements['main'], keyof Own> = $props();

  const classes = $derived(['bb-error-scene', className || null].filter(Boolean).join(' '));
  const hasActions = $derived(Boolean(primary || secondary || actions));
  const code = $derived(perchSplit(status ?? ''));
</script>

{#snippet control(action: ErrorSceneAction, tone: 'primary' | 'quiet')}
  {@const { label, href, ...attributes } = action}
  {#if href}<a class="bb-error-scene__action bb-error-scene__action--{tone}" {href} {...attributes}>{label}</a
    >{:else}<button class="bb-error-scene__action bb-error-scene__action--{tone}" type="button" {...attributes}
      >{label}</button
    >{/if}
{/snippet}

<main class={classes} aria-labelledby={labelledBy} {...rest}>
  <LightField class="bb-light-field--bleed" />

  <div class="bb-error-scene__orbs" aria-hidden="true">
    <span class="bb-orb bb-orb--halo bb-orb--green bb-error-scene__orb"><i class="bb-orb__pulse"></i></span>
    <span class="bb-orb bb-orb--halo bb-orb--tan bb-error-scene__orb bb-error-scene__orb--two"><i class="bb-orb__pulse"></i></span>
  </div>

  <div class="bb-error-scene__content">
    {#if status !== undefined}
      <p class="bb-error-scene__eyebrow"><span>{status}</span> · {eyebrow}</p>
      <p class="bb-error-scene__code" aria-hidden="true"
        >{code.before}<span class="bb-error-scene__perch"
          >{code.digit}{#if mascot}<span class="bb-error-scene__mascot">{@render mascot()}</span>{/if}</span
        >{code.after}</p
      >
    {:else}
      {#if mark}<div class="bb-error-scene__mark" aria-hidden="true">{@render mark()}</div>{/if}
      <p class="bb-error-scene__eyebrow">{eyebrow}</p>
    {/if}
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

    {@render children?.()}
  </div>
</main>
