<script lang="ts">
  import type { Snippet } from 'svelte';
  let { title, action }: { title: string; action?: Snippet } = $props();
</script>

<div class="card-head">
  <h3>{title}</h3>
  {#if action}{@render action()}{/if}
</div>

<style>
  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 16px; }
  h3 { font-family: var(--bb-font-display); font-weight: 700; font-size: 16px; letter-spacing: -0.01em; color: var(--bb-white); margin: 0; }
  .card-head :global(.more) { font-family: var(--bb-font-body); font-weight: 600; font-size: 12.5px; color: var(--bb-tan); cursor: pointer; text-decoration: none; }
  .card-head :global(.more:hover) { color: var(--bb-tan-pale); }
</style>
