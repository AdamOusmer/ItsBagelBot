<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  // Data wrapper around @bagel/ui's `.bb-dock`. The bar, the folding rule and
  // the popover are the library's; the accessible name is this app's copy.
  //
  // `fallbackIcon` is passed explicitly. The version this replaces ended its
  // icon lookup with `?? 'overview'` inside the component -- a name out of
  // this bot's icon set, hard-coded in what is now a library. The library
  // takes no default; the app that owns the set names one.
  import Dock from '@bagel/ui/svelte/Dock.svelte';
  import type { NavGroupDef, NavLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';

  const { t } = getI18n();

  // Log out is deliberately NOT here: it lives in the operator menu, which
  // keeps the dock to pure navigation and phones uncrowded.
  let {
    items,
    groups = [] as NavGroupDef[]
  }: { items: NavLink[]; groups?: NavGroupDef[] } = $props();
</script>

<Dock {items} {groups} ariaLabel={t('nav.ariaMain')} fallbackIcon="overview" />
