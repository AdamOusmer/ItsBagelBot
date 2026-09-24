<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import RailItem from '@bagel/ui/svelte/RailItem.svelte';
  import type { IconName } from '@bagel/ui/lib/icons';
  import { getI18n } from '../lib/i18n/context';

  const { t } = getI18n();

  let {
    href,
    icon,
    label,
    active = false,
    locked = false,
    count
  }: {
    href: string;
    icon?: IconName;
    label: string;
    active?: boolean;
    locked?: boolean;
    count?: string | number;
  } = $props();
</script>

<RailItem
  {href}
  {icon}
  {label}
  {active}
  {locked}
  lockedHint={locked ? t('nav.lockedBroadcaster') : undefined}
  {count}
/>
