<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // Keep the onboarding/deploy stage API while sharing one atmosphere contract.
  import AmbientSky from './AmbientSky.svelte';

  let {
    shift = 0, px = 0, py = 0, progress = 0, leaving = false
  }: {
    shift?: number;
    px?: number;
    py?: number;
    progress?: number;
    leaving?: boolean;
  } = $props();
</script>

<AmbientSky {shift} {px} {py} {progress} {leaving} />
