<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/tabs.css';
  import { mountSectionNav } from '../lib/scroll-spy';

  type Own = {
    label: string;
    items: { href: string; label: string; count?: number; current?: boolean; attrs?: Record<string, string> }[];
    orientation?: 'auto' | 'horizontal' | 'vertical';
    variant?: 'tabs' | 'toc';
    index?: boolean;
    class?: string;
  };

  let {
    label,
    items,
    orientation = 'auto',
    variant = 'tabs',
    index = false,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['nav'], keyof Own> = $props();

  const MODIFIER = {
    auto: 'bb-tabs--auto',
    horizontal: 'bb-tabs--wrap',
    vertical: 'bb-tabs--vertical',
  } as const;

  const classes = $derived(
    ['bb-tabs', variant === 'toc' ? 'bb-tabs--toc' : MODIFIER[orientation], className || null]
      .filter(Boolean)
      .join(' '),
  );
  const routed = $derived(items.some((item) => item.current !== undefined));
  const ordinal = (position: number) => String(position + 1).padStart(2, '0');

  let navEl = $state<HTMLElement | null>(null);
  $effect(() => {
    if (!navEl || routed) return;
    return mountSectionNav(navEl);
  });
</script>

<div class="bb-tabs-host"
  ><nav bind:this={navEl} class={classes} aria-label={label} data-lenis-prevent="" {...rest}
    >{#each items as item, position (item.href)}<a
        class={item.current ? 'bb-tab is-active' : 'bb-tab'}
        href={item.href}
        aria-current={item.current ? 'page' : undefined}
        {...item.attrs}
        >{#if index}<i class="bb-tab__index">{ordinal(position)}</i>{/if}{item.label}{#if item.count != null}<span
            class="bb-tab__count">{item.count}</span
          >{/if}</a
      >{/each}</nav
  ></div
>
