<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  // Data wrapper around @bagel/ui's `.bb-rail-item`. Everything visible about
  // the row -- the height, the icon size, the colour it takes when it is the
  // current page -- is the library's; what is left here is the ONE string the
  // element must never contain.
  //
  // `t('nav.lockedBroadcaster')` used to be called inside the component that
  // drew the row. Resolving it here and handing the result down as `lockedHint`
  // is what lets that element live in a library which cannot know the word
  // "broadcaster", let alone which of this bot's pages are gated on being one.
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
