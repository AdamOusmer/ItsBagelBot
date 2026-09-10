<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-error-scene` contract
  // (../styles/elements/error-scene.css). No Astro twin: an error page is
  // rendered by the app that failed, and both of those are SvelteKit.
  //
  // IT CARRIES NO COPY AND NO STATUS LOGIC, which is the whole reason it is a
  // separate element rather than the console's ErrorView moved wholesale.
  // "404 means the page wandered off" is a sentence about ItsBagelBot; the
  // mapping from an HTTP status to eyebrow/title/description/action lives in
  // web/kit/components/ErrorView.svelte, which renders this.
  //
  // The two actions are a snippet rather than props for the same reason: one of
  // them is sometimes a <button> that reloads, sometimes an <a> to a login
  // route the app names. The element owns their layout and their two tones
  // (`--primary`, `--quiet`), not their behaviour.
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
    /** The status code, drawn oversized and repeated in the eyebrow. */
    status: number | string;
    eyebrow: string;
    title: string;
    description: string;
    /** The quiet footnote under the actions. Optional; omitted renders nothing. */
    aside?: string;
    class?: string;
    /**
     * id given to the title and pointed at by the scene's aria-labelledby.
     * Overridable only so a page rendering two scenes (a preview grid) keeps
     * them distinct.
     */
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
    <!-- The code is aria-hidden: it is an outline treatment
         (-webkit-text-stroke over transparent text) and it is already
         announced, as a number rather than as decoration, in the eyebrow. -->
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
