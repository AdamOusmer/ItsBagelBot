<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { ComponentProps, Snippet } from 'svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Reveal from './Reveal.svelte';

  let { visible = true, tone, role = 'status', children }: {
    visible?: boolean;
    tone?: ComponentProps<typeof AlertBanner>['tone'];
    role?: ComponentProps<typeof AlertBanner>['role'];
    children: Snippet;
  } = $props();
</script>

<Reveal open={visible}>
  <AlertBanner {tone} {role} flush>{@render children()}</AlertBanner>
</Reveal>
