<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/shell.css';
  import Icon from './Icon.svelte';
  import {
    dockGroups,
    groupActive,
    groupCount,
    groupIcon,
    hoistHome,
    isGrouped,
  } from '../lib/dock-groups';
  import type { IconName } from '../lib/icons';
  import type { UiNavGroup, UiNavLink } from '../lib/nav-types';

  let {
    items = [],
    groups = [],
    ariaLabel,
    homeHref = '/',
    fallbackIcon,
    class: className = '',
    ...rest
  }: {
    items?: UiNavLink[];
    groups?: UiNavGroup[];
    ariaLabel?: string;
    homeHref?: string;
    fallbackIcon?: IconName;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-dock', className || null].filter(Boolean).join(' '));

  const grouped = $derived(isGrouped(groups));
  const home = $derived(grouped ? hoistHome(groups, homeHref) : null);
  const folded = $derived(grouped ? dockGroups(groups, homeHref) : []);
  const flat = $derived(grouped ? [] : items);

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

<nav class={classes} aria-label={ariaLabel} {...rest}
  ><div class="bb-dock__inner"
    >{#if home}<a
        class="bb-dock-item"
        href={home.href}
        data-active={home.active ? '' : undefined}
        aria-current={home.active ? 'page' : undefined}
        onclick={closeOnNav}
        >{#if home.icon}<Icon name={home.icon} size={18} />{/if}<span
          class="bb-dock-item__label">{home.label}</span
        ></a
      >{/if}{#each folded as group (group.label)}{#if group.items.length === 1}{@const item =
        group.items[0]}<a
          class="bb-dock-item"
          href={item.href}
          data-active={item.active ? '' : undefined}
          aria-current={item.active ? 'page' : undefined}
          onclick={closeOnNav}
          >{#if item.icon}<Icon name={item.icon} size={18} />{/if}<span
            class="bb-dock-item__label">{item.label}</span
          >{#if item.count}<span class="bb-dock-item__count" aria-hidden="true"
              >{item.count}</span
            >{/if}</a
        >{:else}<div class="bb-dock__group"
          ><button
            type="button"
            class="bb-dock-item"
            data-active={groupActive(group) ? '' : undefined}
            aria-expanded={openGroup === group.label}
            aria-haspopup="menu"
            onclick={() => toggleGroup(group.label ?? '')}
            >{#if groupIcon(group, fallbackIcon)}<Icon
                name={groupIcon(group, fallbackIcon) as IconName}
                size={18}
              />{/if}<span class="bb-dock-item__label">{group.label}</span
            >{#if groupCount(group)}<span class="bb-dock-item__count" aria-hidden="true"
                >{groupCount(group)}</span
              >{/if}</button
          >{#if openGroup === group.label}<div class="bb-dock__popover" role="menu"
              >{#each group.items as item (item.href)}<a
                  class="bb-dock__pop-item"
                  href={item.href}
                  role="menuitem"
                  data-active={item.active ? '' : undefined}
                  aria-current={item.active ? 'page' : undefined}
                  onclick={closeOnNav}
                  >{#if item.icon}<Icon name={item.icon} />{/if}<span>{item.label}</span
                  >{#if item.count}<span class="bb-dock__pop-count">{item.count}</span
                    >{/if}</a
                >{/each}</div
            >{/if}</div
        >{/if}{/each}{#each flat as item (item.href)}<a
        class="bb-dock-item"
        href={item.href}
        data-active={item.active ? '' : undefined}
        aria-current={item.active ? 'page' : undefined}
        onclick={closeOnNav}
        >{#if item.icon}<Icon name={item.icon} size={18} />{/if}<span
          class="bb-dock-item__label">{item.label}</span
        >{#if item.count}<span class="bb-dock-item__count" aria-hidden="true"
            >{item.count}</span
          >{/if}</a
      >{/each}</div
  ></nav
>
