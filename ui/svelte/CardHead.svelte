<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // The section header inside a card: Syne title on the left, one quiet action
  // on the right. Its Astro twin is ../astro/CardHead.astro.
  //
  // The action slot used to be styled through a `:global(.more)` escape hatch,
  // with console.css carrying a byte-equivalent copy of the same rule under a
  // global selector. Both are gone: the link takes the contract class
  // `bb-card-head__more` and the slot can hold anything else untouched.
  import type { Snippet } from 'svelte';
  import '../styles/elements/card.css';

  let { title, action }: { title: string; action?: Snippet } = $props();
</script>

<div class="bb-card-head">
  <h3 class="bb-card-head__title">{title}</h3>
  {#if action}{@render action()}{/if}
</div>
