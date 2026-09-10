<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-page-head`. Its Astro twin is
  // ../astro/PageHead.astro; ../test/parity.test.ts diffs the two.
  import '../styles/elements/shell.css';
  import type { Snippet } from 'svelte';

  let {
    eyebrow,
    title,
    description,
    children,
    trail,
    class: className = '',
    ...rest
  }: {
    eyebrow?: string;
    /** The heading. `children` wins when both are given (rich titles). */
    title?: string;
    description?: string;
    children?: Snippet;
    /** A right-hand strip of page-level readouts. */
    trail?: Snippet;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-page-head', trail ? 'bb-page-head--trailed' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div class={classes} {...rest}
  ><div class="bb-page-head__main"
    >{#if eyebrow}<span class="bb-page-head__eyebrow">{eyebrow}</span>{/if}<!--
    tabindex="-1" so an SPA route change can move focus to the page title; a
    programmatic .focus() does not trigger :focus-visible, so no ring shows.
    --><h1 tabindex="-1">{#if children}{@render children()}{:else}{title}{/if}</h1
    >{#if description}<p class="bb-page-head__description">{description}</p>{/if}</div
  >{#if trail}<div class="bb-page-head__trail">{@render trail()}</div>{/if}</div
>
