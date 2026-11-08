<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import DonationCornerCard from './DonationCornerCard.svelte';
  let { children }: { children: Snippet } = $props();
</script>

<header class="streamer-head">
  <div class="streamer-head__intro">{@render children()}</div>
  <div class="streamer-head__support"><DonationCornerCard /></div>
</header>

<style>
  .streamer-head { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: var(--bb-space-6); width: 100%; }
  .streamer-head__intro { min-width: 0; }
  .streamer-head__support { padding-top: var(--bb-space-5); }
  @media (max-width: 600px) {
    .streamer-head { grid-template-columns: minmax(0, 1fr); gap: var(--bb-space-5); }
    .streamer-head__support { width: 100%; padding-top: var(--bb-space-3); }
  }
</style>
