<script lang="ts">
  import type { Snippet } from 'svelte';
  let { title, action }: { title: string; action?: Snippet } = $props();
</script>

<div class="card-head">
  <h3>{title}</h3>
  {#if action}{@render action()}{/if}
</div>

<style>
  .card-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 18px; }
  h3 { font-family: var(--bb-font-display); font-weight: 600; font-size: 17px; color: var(--bb-white); margin: 0; letter-spacing: -0.01em; }
  .card-head :global(.more) { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.06em; color: var(--bb-tan); cursor: pointer; }
</style>
