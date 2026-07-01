<script lang="ts">
  import type { Snippet } from 'svelte';
  let { title, action }: { title: string; action?: Snippet } = $props();
</script>

<div class="card-head">
  <h3>{title}</h3>
  {#if action}{@render action()}{/if}
</div>

<style>
  /* Ledger section header: mono uppercase legend on a rule. */
  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 16px; padding-bottom: 12px; border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.1)); }
  h3 { font-family: var(--bb-font-mono); font-weight: 500; font-size: 12px; letter-spacing: 0.16em; text-transform: uppercase; color: var(--bb-white); margin: 0; }
  .card-head :global(.more) { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.06em; color: var(--bb-tan); cursor: pointer; text-decoration: none; }
  .card-head :global(.more:hover) { color: var(--bb-tan-pale); }
</style>
