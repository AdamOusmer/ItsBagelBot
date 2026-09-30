<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import { getUiI18n } from './i18n';
  import '../styles/elements/shell.css';
  import Brand from './Brand.svelte';
  import RailItem from './RailItem.svelte';
  import { mountGlide } from '../lib/rail-glide';
  import type { Snippet } from 'svelte';
  import type { UiBrand, UiNavGroup, UiNavLink } from '../lib/nav-types';

  const i18n = getUiI18n();
  type Own = {
    brand: UiBrand;
    groups: UiNavGroup[];
    footer?: Snippet;
    label?: string;
    class?: string;
  };

  let {
    brand,
    groups,
    footer,
    label = i18n.t('nav.main'),
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['aside'], keyof Own> = $props();

  const classes = $derived(['bb-rail', className || null].filter(Boolean).join(' '));

  let manual = $state<Record<string, boolean>>({});
  const isOpen = (item: UiNavLink) => manual[item.href ?? ''] ?? !!item.current;
  const toggle = (item: UiNavLink) => {
    manual[item.href ?? ''] = !isOpen(item);
  };

  let railEl = $state<HTMLElement | null>(null);
  let navEl = $state<HTMLElement | null>(null);

  $effect(() => {
    if (!railEl) return;
    void groups;
    void manual;
    return mountGlide(railEl, { navEl });
  });
</script>

<aside bind:this={railEl} class={classes} aria-label={label} {...rest}
  ><Brand
    title={brand.title}
    sub={brand.sub}
    href={brand.href}
    logoSrc={brand.logoSrc}
    logoAlt={brand.logoAlt}
    logoShape={brand.logoShape}
  /><span class="bb-rail__glide" aria-hidden="true"
    ><span class="bb-rail__glide-edge"></span></span
  ><div bind:this={navEl} class="bb-rail__nav"
    >{#each groups as group (group.label ?? '')}{#if group.label}<div
          class="bb-rail__label">{group.label}</div
        >{/if}<div class="bb-rail__group"
        >{#each group.items as item (item.href)}{#if item.children && item.children.length}<div
              class="bb-rail__row"
              ><RailItem
                href={item.href}
                icon={item.icon}
                label={item.label}
                current={item.current}
                locked={item.locked}
                lockedHint={item.lockedHint}
                count={item.count}
              /><button
                class="bb-rail__chev"
                type="button"
                aria-expanded={isOpen(item)}
                aria-label={item.label}
                onclick={() => toggle(item)}
                ><svg viewBox="0 0 24 24" width="14" height="14"
                  ><polyline points="9 6 15 12 9 18"></polyline></svg
                ></button
              ></div
            ><div class="bb-rail__subs" data-open={isOpen(item) ? '' : undefined}
              ><div class="bb-rail__subs-inner"
                >{#each item.children as child (child.href)}<a
                    class="bb-rail__sub"
                    href={child.href}
                    ><span class="bb-rail__sub-label">{child.label}</span>{#if child.count !== undefined}<span
                        class="bb-rail__sub-count">{child.count}</span
                      >{/if}</a
                  >{/each}</div
              ></div
            >{:else}<RailItem
              href={item.href}
              icon={item.icon}
              label={item.label}
              current={item.current}
              locked={item.locked}
              lockedHint={item.lockedHint}
              count={item.count}
            />{/if}{/each}</div
      >{/each}</div
  ><div class="bb-rail__spacer"></div>{#if footer}{@render footer()}{/if}</aside
>
