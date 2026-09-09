<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Settings keeps this local name; the markup lives in shared SectionNav
  // (the modules index uses the same component).
  import { SectionNav } from '@bagel/shared';
  let {
    label,
    items
  }: {
    label: string;
    items: { href: string; label: string }[];
  } = $props();
</script>

<SectionNav {label} {items} />
