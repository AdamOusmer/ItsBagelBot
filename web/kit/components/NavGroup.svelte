<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  // Data wrapper around @bagel/ui's `.bb-nav-group`: the numbered register.
  // It exists for the same one reason RailItem's does -- the locked entries'
  // hint is this bot's copy, resolved here so the element never holds it.
  import NavGroup from '@bagel/ui/svelte/NavGroup.svelte';
  import type { NavLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';

  const { t } = getI18n();

  let { label, items, startIndex = 1 }: { label?: string; items: NavLink[]; startIndex?: number } =
    $props();

  const withHints = $derived(
    items.map((item) => (item.locked ? { ...item, lockedHint: t('nav.lockedBroadcaster') } : item))
  );
</script>

<NavGroup {label} items={withHints} {startIndex} />
