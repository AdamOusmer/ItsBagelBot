<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import NavGroup from '@bagel/ui/svelte/NavGroup.svelte';
  import type { NavLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';
  import { withLockedHints } from '../lib/nav-core';

  const { t } = getI18n();

  let { label, items, startIndex = 1 }: { label?: string; items: NavLink[]; startIndex?: number } =
    $props();

  const withHints = $derived(withLockedHints(items, t));
</script>

<NavGroup {label} items={withHints} {startIndex} />
