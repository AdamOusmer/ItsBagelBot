<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import NavLink from '@bagel/ui/svelte/NavLink.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import type { IconName } from '@bagel/ui/lib/icons';
  import { getI18n } from '../lib/i18n/context';

  const { t } = getI18n();

  let {
    href,
    icon: iconName,
    label,
    active = false,
    locked = false,
    count,
    index
  }: {
    href: string;
    icon?: IconName;
    label: string;
    active?: boolean;
    locked?: boolean;
    count?: string | number;
    index?: number;
  } = $props();

  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : undefined);
</script>

<NavLink
  class="nav-item"
  href={locked ? undefined : href}
  {label}
  current={active}
  disabled={locked}
  hint={locked ? t('nav.lockedBroadcaster') : undefined}
  block
>
  {#snippet icon()}
    {#if idx}<span class="bb-nav-link__index" class:bb-nav-link__index--current={active}>{idx}</span>{/if}
    {#if iconName}<Icon name={iconName} />{/if}
  {/snippet}
  {#snippet trail()}
    {#if locked}<Icon name="lock" size={13} />{/if}
    {#if count !== undefined}<span class="bb-nav-link__count">{count}</span>{/if}
  {/snippet}
</NavLink>
