<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import { getUiI18n } from './i18n';
  import '../styles/elements/shell.css';
  import Icon from './Icon.svelte';
  import {
    dockGroups,
    groupActive,
    groupCount,
    groupIcon,
    hoistHome,
    isGrouped,
    overflowStart,
  } from '../lib/dock-groups';
  import type { IconName } from '../lib/icons';
  import type { UiNavGroup, UiNavLink } from '../lib/nav-types';

  const i18n = getUiI18n();
  type Own = {
    items?: UiNavLink[];
    groups?: UiNavGroup[];
    label?: string;
    homeHref?: string;
    fallbackIcon?: IconName;
    quick?: number;
    class?: string;
  };

  let {
    items = [],
    groups = [],
    label = i18n.t('nav.main'),
    homeHref = '/',
    fallbackIcon,
    quick,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['nav'], keyof Own> = $props();

  const classes = $derived(['bb-dock', className || null].filter(Boolean).join(' '));

  const grouped = $derived(isGrouped(groups));
  const home = $derived(grouped ? hoistHome(groups, homeHref) : null);
  const folded = $derived(grouped ? dockGroups(groups, homeHref) : []);
  const flat = $derived(grouped ? [] : items);
  const cut = $derived(overflowStart(flat.length, quick));
  const overflow = $derived<UiNavGroup | null>(
    cut < flat.length ? { label: i18n.t('nav.menu'), items: flat.slice(cut) } : null,
  );

  let openGroup = $state<string | null>(null);
  const toggleGroup = (label: string) => (openGroup = openGroup === label ? null : label);
  const closeOnNav = () => (openGroup = null);
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key === 'Escape') openGroup = null;
  }}
/>

{#if openGroup}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="bb-dock__scrim"
    role="presentation"
    onclick={closeOnNav}
    onkeydown={(e) => {
      if (e.key === 'Enter') closeOnNav();
    }}
  ></div>
{/if}

{#snippet caption(text: string)}<span class="bb-dock-item__label"
    ><span data-fit="block" data-fit-group="dock">{text}</span></span
  >{/snippet}

{#snippet link(item: UiNavLink, fold: boolean)}<a
    class="bb-dock-item"
    href={item.href}
    data-active={item.current ? '' : undefined}
    aria-current={item.current ? 'page' : undefined}
    data-dock-fold={fold ? '' : undefined}
    onclick={closeOnNav}
    >{#if item.icon}<Icon name={item.icon} size={18} />{/if}{@render caption(item.label)}{#if item.count}<span
        class="bb-dock-item__count"
        aria-hidden="true">{item.count}</span
      >{/if}</a
  >{/snippet}

{#snippet groupSlot(group: UiNavGroup, icon: IconName | undefined, more: boolean)}<div
    class={more ? 'bb-dock__group bb-dock__more' : 'bb-dock__group'}
    ><button
      type="button"
      class="bb-dock-item"
      data-active={groupActive(group) ? '' : undefined}
      aria-expanded={openGroup === group.label}
      aria-haspopup="menu"
      onclick={() => toggleGroup(group.label ?? '')}
      >{#if icon}<Icon name={icon} size={18} />{/if}{@render caption(group.label ?? '')}{#if groupCount(group)}<span
          class="bb-dock-item__count"
          aria-hidden="true">{groupCount(group)}</span
        >{/if}</button
    >{#if openGroup === group.label}<div class="bb-dock__popover" role="menu"
        >{#each group.items as item (item.href)}<a
            class="bb-dock__pop-item"
            href={item.href}
            role="menuitem"
            data-active={item.current ? '' : undefined}
            aria-current={item.current ? 'page' : undefined}
            onclick={closeOnNav}
            >{#if item.icon}<Icon name={item.icon} />{/if}<span>{item.label}</span
            >{#if item.count}<span class="bb-dock__pop-count">{item.count}</span>{/if}</a
          >{/each}</div
      >{/if}</div
  >{/snippet}

<nav class={classes} aria-label={label} {...rest}
  ><div class="bb-dock__inner"
    >{#if home}{@render link(home, false)}{/if}{#each folded as group (group.label)}{#if group.items.length === 1}{@render link(
          group.items[0],
          false,
        )}{:else}{@render groupSlot(group, groupIcon(group, fallbackIcon), false)}{/if}{/each}{#each flat as item, index (item.href)}{@render link(
        item,
        index >= cut,
      )}{/each}{#if overflow}{@render groupSlot(overflow, 'list', true)}{/if}</div
  ></nav
>
