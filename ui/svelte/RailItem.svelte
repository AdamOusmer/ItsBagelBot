<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/shell.css';
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';

  type Own = {
    href?: string;
    icon?: IconName;
    label: string;
    current?: boolean;
    locked?: boolean;
    lockedHint?: string;
    count?: string | number;
    class?: string;
  };

  let {
    href,
    icon,
    label,
    current = false,
    locked = false,
    lockedHint,
    count,
    class: className = '',
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

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
    data-active={current ? '' : undefined}
    aria-current={current ? 'page' : undefined}
    {...rest}
    >{#if icon}<Icon name={icon} />{/if}<span class="bb-rail-item__label">{label}</span>{#if count !== undefined}<span
        class="bb-rail-item__count">{count}</span
      >{/if}</a
  >
{/if}
