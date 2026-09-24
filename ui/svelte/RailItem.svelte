<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/shell.css';
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';

  let {
    href,
    icon,
    label,
    active = false,
    locked = false,
    lockedHint,
    count,
    class: className = '',
    ...rest
  }: {
    href?: string;
    icon?: IconName;
    label: string;
    active?: boolean;
    locked?: boolean;
    lockedHint?: string;
    count?: string | number;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-rail-item', className || null].filter(Boolean).join(' '));
</script>

{#if locked}
  <span class={classes} data-locked="" {...rest}
    >{#if icon}<Icon name={icon} />{/if}<span class="bb-rail-item__label">{label}</span><Icon
      name="lock"
      size={13}
    />{#if lockedHint}<span class="bb-rail-item__hint">{lockedHint}</span>{/if}</span
  >
{:else}
  <a
    class={classes}
    {href}
    data-active={active ? '' : undefined}
    aria-current={active ? 'page' : undefined}
    {...rest}
    >{#if icon}<Icon name={icon} />{/if}<span class="bb-rail-item__label">{label}</span>{#if count !== undefined}<span
        class="bb-rail-item__count">{count}</span
      >{/if}</a
  >
{/if}
