<script lang="ts">
  // Standardized row-list container card. Its only job is guaranteeing identical
  // container padding + class across list pages (timers/commands/etc). The row
  // items themselves are supplied by callers as children.
  import type { Snippet } from 'svelte';
  import Card from './Card.svelte';
  let { children }: { children: Snippet } = $props();
</script>

<Card class="deck-list" style="padding:6px 0 0">
  {@render children()}
</Card>

<style>
  /* Marker hook for callers that target rows inside the list (e.g.
     `.deck-list :global(.row-shell:last-child)`). Container styling itself
     rides on Card + the inline padding above. */
  :global(.deck-list) { display: block; }
</style>
