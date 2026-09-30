<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import type { Snippet } from 'svelte';
  import '../styles/elements/card.css';

  let { title, actions }: { title: string; actions?: Snippet } = $props();
</script>

<div class="bb-card-head">
  <h3 class="bb-card-head__title">{title}</h3>
  {#if actions}{@render actions()}{/if}
</div>
