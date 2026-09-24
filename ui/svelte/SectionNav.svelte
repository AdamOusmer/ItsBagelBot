<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/tags.css';
  import { mountHashActive } from '../lib/hash-active';

  let {
    label,
    items,
    orientation = 'auto',
    class: className = '',
    ...rest
  }: {
    label: string;
    items: { href: string; label: string; count?: number }[];
    orientation?: 'auto' | 'horizontal' | 'vertical';
    class?: string;
    [key: string]: unknown;
  } = $props();

  const MODIFIER = {
    auto: 'bb-tabs--auto',
    horizontal: 'bb-tabs--wrap',
    vertical: 'bb-tabs--vertical',
  } as const;

  const classes = $derived(
    ['bb-tabs', MODIFIER[orientation], className || null].filter(Boolean).join(' '),
  );

  let navEl = $state<HTMLElement | null>(null);
  $effect(() => {
    if (!navEl) return;
    return mountHashActive(navEl);
  });
</script>

<div class="bb-tabs-host"
  ><nav bind:this={navEl} class={classes} aria-label={label} {...rest}
    >{#each items as item (item.href)}<a class="bb-tab" href={item.href}
        >{item.label}{#if item.count != null}<span class="bb-tab__count"
            >{item.count}</span
          >{/if}</a
      >{/each}</nav
  ></div
>
