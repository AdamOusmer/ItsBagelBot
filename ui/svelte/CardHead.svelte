<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import type { Snippet } from 'svelte';
  import '../styles/elements/card.css';

  let { title, headingLevel = 3, actions }: { title: string; headingLevel?: 1 | 2 | 3 | 4 | 5 | 6; actions?: Snippet } = $props();
</script>

<div class="bb-card-head">
  <svelte:element this={`h${headingLevel}`} class="bb-card-head__title">{title}</svelte:element>
  {#if actions}{@render actions()}{/if}
</div>
