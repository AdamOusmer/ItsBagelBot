<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/collapse.css';
  import type { Snippet } from 'svelte';

  let { open, children }: { open: boolean; children: Snippet } = $props();
</script>

<div class="bb-collapse" class:is-open={open} inert={!open} aria-hidden={!open}>
  <div>{@render children()}</div>
</div>
