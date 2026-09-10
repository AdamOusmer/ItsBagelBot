<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  let { eyebrow, title, description, children, trail }:
    { eyebrow?: string; title?: string; description?: string; children?: Snippet; trail?: Snippet } = $props();
</script>

<div class="page-head" class:has-trail={!!trail}>
  <div class="head-main">
    {#if eyebrow}<span class="eyebrow">{eyebrow}</span>{/if}
    <!-- tabindex=-1 so SPA route-change focus can land on the page title; a
         programmatic .focus() does not trigger :focus-visible, so no ring shows. -->
    <h1 tabindex="-1">{#if children}{@render children()}{:else}{title}{/if}</h1>
    {#if description}<p>{description}</p>{/if}
  </div>
  <!-- Optional right-hand strip (page-level readouts). Bottom-aligned with the
       description so the title block stays the anchor at every width. -->
  {#if trail}<div class="head-trail">{@render trail()}</div>{/if}
</div>

<style>
  /* Mirrors the marketing site's SectionHeading: quiet mono green eyebrow,
     Syne 800 title with gentle tracking. No rules, no ornaments. */
  .page-head { margin-bottom: calc(30px * var(--d)); }
  /* Flex only when a trail is present, so pages without one keep the exact
     block layout (and margin collapse) they had before the slot existed. */
  .page-head.has-trail {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: 32px;
    flex-wrap: wrap;
  }
  .head-main { min-width: 0; }
  .head-trail { flex: none; }
  .eyebrow { font-family: var(--bb-font-mono); font-size: 0.72rem; letter-spacing: 0.14em; text-transform: uppercase; color: var(--bb-green-glow); display: block; margin-bottom: 14px; }
  h1 { font-family: var(--bb-font-display); font-weight: 800; font-size: clamp(32px, 4vw, 48px); line-height: 1.05; letter-spacing: -0.01em; color: var(--bb-white); margin: 0; }
  h1 :global(em) { font-style: normal; font-weight: 800; color: var(--bb-tan-light); font-family: var(--bb-font-display); }
  p { font-family: var(--bb-font-body); font-size: 14.5px; line-height: 1.55; color: var(--bb-muted); margin: 12px 0 0; max-width: 560px; }
</style>
