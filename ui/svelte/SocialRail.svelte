<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/nav.css';
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';

  let {
    items,
    ariaLabel,
    size = 17,
    class: className = '',
    ...rest
  }: {
    items: { label: string; href: string; icon: IconName }[];
    ariaLabel: string;
    size?: number;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-social-rail', className || null].filter(Boolean).join(' '));
</script>

<aside class={classes} aria-label={ariaLabel} {...rest}
  >{#each items as item (item.href)}<a
      class="bb-social-rail__item"
      href={item.href}
      aria-label={item.label}
      title={item.label}
      target="_blank"
      rel="noopener noreferrer"><Icon name={item.icon} {size} /></a
    >{/each}</aside
>
