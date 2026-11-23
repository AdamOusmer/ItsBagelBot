<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { ComponentProps, Snippet } from 'svelte';
  import '@bagel/ui/styles/elements/collapse.css';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';

  let { visible = true, tone, role, gap = '0px', children }: {
    visible?: boolean;
    tone?: ComponentProps<typeof AlertBanner>['tone'];
    role?: ComponentProps<typeof AlertBanner>['role'];
    gap?: string;
    children: Snippet;
  } = $props();
</script>

<div class="notice bb-collapse" class:is-open={visible} inert={!visible} aria-hidden={!visible} style:--bb-collapse-gap={gap}>
  <div>
    <AlertBanner {tone} {role} flush>
      <span class="copy"><bb-fit mode="block">{@render children()}</bb-fit></span>
    </AlertBanner>
  </div>
</div>

<style>
  .notice { min-inline-size: 0; }
  .copy { display: block; block-size: 2.75em; align-content: safe center; overflow: auto; overflow-wrap: anywhere; }
</style>
