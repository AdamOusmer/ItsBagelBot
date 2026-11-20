<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { ComponentProps, Snippet } from 'svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';

  let { visible = true, tone, role, children }: {
    visible?: boolean;
    tone?: ComponentProps<typeof AlertBanner>['tone'];
    role?: ComponentProps<typeof AlertBanner>['role'];
    children: Snippet;
  } = $props();
</script>

<div class="notice" class:faded={!visible} inert={!visible} aria-hidden={!visible}>
  <AlertBanner {tone} {role} flush>
    <span class="copy"><bb-fit mode="block">{@render children()}</bb-fit></span>
  </AlertBanner>
</div>

<style>
  .notice { min-inline-size: 0; transition: opacity var(--bb-dur-fast); }
  .faded { opacity: 0; }
  .copy { display: block; block-size: 2.75em; align-content: safe center; overflow: auto; overflow-wrap: anywhere; }
  @media (prefers-reduced-motion: reduce) {
    .notice { transition: none; }
  }
</style>
