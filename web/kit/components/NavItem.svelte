<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  // The console rail's ledger entry. Presentation is @bagel/ui's `.bb-nav-link`
  // -- the same element the marketing bar, the mobile menu and the footer
  // columns render -- and what stays here is the bot-specific half this
  // component has always owned: the nav registry's shape (icon name, index,
  // count, broadcaster lock) and the one i18n string the lock needs.
  //
  // That string is why this wrapper exists at all rather than the rail
  // rendering NavLink directly. `t('nav.lockedBroadcaster')` used to be called
  // INSIDE the component that drew the link; resolving it here and handing the
  // result down as `hint` is what lets the element live in a library that must
  // never know the word "broadcaster".
  //
  // The ledger's own active mark is gone with the CSS: the green square pinned
  // to the right edge of the active row is now the contract's tan diamond on
  // the left, next to the index, which is where every other surface marks the
  // current page. One active state, one place to look for it.
  import NavLink from '@bagel/ui/svelte/NavLink.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import type { IconName } from '@bagel/ui/lib/icons';
  import { getI18n } from '../lib/i18n/context';

  const { t } = getI18n();

  // `icon` is destructured under another name because the markup below passes a
  // SNIPPET called `icon` to NavLink, and a snippet shadows a prop of the same
  // name inside its own body.
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
