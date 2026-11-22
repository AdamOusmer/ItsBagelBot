<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/collapse.css';
  import type { ComponentProps, Snippet } from 'svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';

  let { visible = true, tone, role = 'status', children }: {
    visible?: boolean;
    tone?: ComponentProps<typeof AlertBanner>['tone'];
    role?: ComponentProps<typeof AlertBanner>['role'];
    children: Snippet;
  } = $props();
</script>

<div class="bb-collapse" class:is-open={visible} inert={!visible} aria-hidden={!visible}>
  <div>
    <AlertBanner {tone} {role} flush>{@render children()}</AlertBanner>
  </div>
</div>
