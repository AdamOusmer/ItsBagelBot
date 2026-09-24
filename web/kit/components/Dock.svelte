<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Dock from '@bagel/ui/svelte/Dock.svelte';
  import type { NavGroupDef, NavLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';

  const { t } = getI18n();

  let {
    items,
    groups = [] as NavGroupDef[]
  }: { items: NavLink[]; groups?: NavGroupDef[] } = $props();
</script>

<Dock {items} {groups} ariaLabel={t('nav.ariaMain')} fallbackIcon="overview" />
