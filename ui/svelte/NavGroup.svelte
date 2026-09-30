<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/nav.css';
  import NavLink from './NavLink.svelte';
  import Icon from './Icon.svelte';
  import type { UiNavLink } from '../lib/nav-types';

  type Own = {
    label?: string;
    items: UiNavLink[];
    startIndex?: number;
    class?: string;
  };

  let {
    label,
    items,
    startIndex = 1,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['nav'], keyof Own> = $props();

  const classes = $derived(['bb-nav-group', className || null].filter(Boolean).join(' '));

  const pad = (n: number) => String(n).padStart(2, '0');
</script>

{#if label}<div class="bb-nav-group__label">{label}</div>{/if}<nav
  class={classes}
  {...rest}
  >{#each items as item, i (item.href ?? item.label)}<NavLink
      href={item.locked ? undefined : item.href}
      label={item.label}
      current={item.current}
      disabled={item.locked}
      hint={item.locked ? item.lockedHint : undefined}
      block
      >{#snippet leading()}<span
          class="bb-nav-group__index"
          data-active={item.current ? '' : undefined}>{pad(startIndex + i)}</span
        >{#if item.icon}<Icon name={item.icon} />{/if}{/snippet}{#snippet trailing()}{#if item.locked}<Icon
            name="lock"
            size={13}
          />{/if}{#if item.count !== undefined}<span class="bb-nav-group__count"
            >{item.count}</span
          >{/if}{/snippet}</NavLink
    >{/each}</nav
>
