<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { navigating } from '$app/state';

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

<div class="bb-nav-progress" class:bb-nav-progress--active={active} role="presentation" aria-hidden="true"></div>
