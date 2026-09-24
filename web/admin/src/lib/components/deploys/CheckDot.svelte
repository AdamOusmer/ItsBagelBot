<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import type { Tone } from './view';

  let { tone, label }: { tone: Tone; label: string } = $props();
</script>

<span class="check-dot" role="img" aria-label={label} title={label}><StatusDot {tone} /></span>

<style>
  .check-dot {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 12px;
    height: 12px;
  }
</style>
