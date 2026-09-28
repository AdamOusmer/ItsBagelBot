<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { AlertBanner } from '@bagel/ui/svelte';
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';

  let { message }: { message: string } = $props();
  let el = $state<HTMLDivElement | null>(null);

  $effect(() => {
    if (!message || !el) return;
    el.scrollIntoView({ block: 'center', behavior: prefersReducedMotion() ? 'auto' : 'smooth' });
    el.focus({ preventScroll: true });
  });
</script>

<div class="import-alert" tabindex="-1" bind:this={el}>
  <AlertBanner>{message}</AlertBanner>
</div>

<style>
  .import-alert:focus { outline: none; }
  .import-alert:focus-visible { outline: 2px solid var(--bb-tan); outline-offset: 2px; border-radius: var(--bb-radius-sm); }
</style>
