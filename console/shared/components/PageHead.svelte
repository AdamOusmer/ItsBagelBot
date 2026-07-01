<script lang="ts">
  import type { Snippet } from 'svelte';
  let { eyebrow, title, description, children }:
    { eyebrow?: string; title?: string; description?: string; children?: Snippet } = $props();
</script>

<div class="page-head">
  {#if eyebrow}<span class="eyebrow">{eyebrow}</span>{/if}
  <h1>{#if children}{@render children()}{:else}{title}{/if}</h1>
  {#if description}<p>{description}</p>{/if}
</div>

<style>
  /* Masthead: oversized display type sitting on a baseline rule with a short
     tan tick — the page reads like the header row of a broadcast log. */
  .page-head { margin-bottom: calc(30px * var(--d)); padding-bottom: calc(20px * var(--d)); border-bottom: 1px solid var(--rule, rgba(240,236,228,0.1)); position: relative; }
  .page-head::after { content: ""; position: absolute; bottom: -1px; left: 0; width: 64px; height: 1px; background: var(--bb-tan); }
  .eyebrow { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.22em; text-transform: uppercase; color: var(--bb-green-glow); display: flex; align-items: center; gap: 10px; margin-bottom: 14px; }
  .eyebrow::before { content: "//"; color: var(--bb-tan); letter-spacing: 0; }
  h1 { font-family: var(--bb-font-display); font-weight: 800; font-size: clamp(34px, 4.2vw, 56px); line-height: 0.98; letter-spacing: -0.03em; color: var(--bb-white); margin: 0; }
  h1 :global(em) { font-style: normal; font-weight: 800; color: var(--bb-tan-light); font-family: var(--bb-font-display); }
  p { font-family: var(--bb-font-body); font-size: 14.5px; line-height: 1.55; color: var(--bb-muted); margin: 12px 0 0; max-width: 560px; }
</style>
