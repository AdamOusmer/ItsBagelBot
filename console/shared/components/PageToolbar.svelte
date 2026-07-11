<script lang="ts">
  // Horizontal control row for a page header area: a lead slot, a flexible
  // spacer, and a trail slot. Self-contained (does not lean on the global
  // .toolbar rule) so it looks identical wherever it's dropped.
  import type { Snippet } from 'svelte';
  let { lead, trail }: { lead?: Snippet; trail?: Snippet } = $props();
</script>

<div class="toolbar">
  {#if lead}{@render lead()}{/if}
  <div class="grow"></div>
  {#if trail}{@render trail()}{/if}
</div>

<style>
  .toolbar { display: flex; align-items: center; gap: 12px; margin-bottom: 18px; }
  .grow { flex: 1; }
  @media (max-width: 600px) {
    .toolbar { flex-wrap: wrap; }
  }
</style>
