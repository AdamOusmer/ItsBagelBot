<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { navigating } from '$app/state';
  import NavProgress from '@bagel/ui/svelte/NavProgress.svelte';

  const SHOW_AFTER_MS = 150;

  let active = $state(false);

  $effect(() => {
    if (!navigating.to) {
      active = false;
      return;
    }
    const timer = setTimeout(() => (active = true), SHOW_AFTER_MS);
    return () => clearTimeout(timer);
  });
</script>

<NavProgress {active} />
