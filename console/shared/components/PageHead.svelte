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
  .page-head { margin-bottom: calc(28px * var(--d)); }
  .eyebrow { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.2em; text-transform: uppercase; color: var(--bb-green-glow); display: block; margin-bottom: 12px; }
  h1 { font-family: var(--bb-font-display); font-weight: 700; font-size: clamp(30px, 3.4vw, 42px); line-height: 1.04; letter-spacing: -0.025em; color: var(--bb-white); margin: 0; }
  h1 :global(em) { font-style: normal; font-weight: 700; color: var(--bb-tan-light); font-family: var(--bb-font-display); }
  p { font-family: var(--bb-font-body); font-size: 15px; line-height: 1.55; color: var(--bb-muted); margin: 12px 0 0; max-width: 560px; }
</style>
