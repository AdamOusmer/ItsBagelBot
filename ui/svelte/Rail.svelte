<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import { getUiI18n } from './i18n';
  import '../styles/elements/shell.css';
  import Brand from './Brand.svelte';
  import RailItem from './RailItem.svelte';
  import Glide from './Glide.svelte';
  import Icon from './Icon.svelte';
  import ProfileMenu from './ProfileMenu.svelte';
  import Bolota from './Bolota.svelte';
  import { mountGlide } from '../lib/glide';
  import type { Snippet } from 'svelte';
  import type { UiAccount, UiBrand, UiNavGroup, UiNavLink } from '../lib/nav-types';

  const i18n = getUiI18n();
  type Own = {
    brand: UiBrand;
    groups: UiNavGroup[];
    account?: UiAccount;
    accountOpen?: boolean;
    onnavigate?: (href: string) => void;
    footer?: Snippet;
    label?: string;
    collapsible?: boolean;
    collapsed?: boolean;
    alwaysVisible?: boolean;
    topInset?: number;
    collapsedWidth?: number;
    class?: string;
  };

  let {
    brand,
    groups,
    account,
    accountOpen = $bindable(false),
    onnavigate,
    footer,
    label = i18n.t('nav.main'),
    collapsible = false,
    collapsed = $bindable(false),
    alwaysVisible = false,
    topInset,
    collapsedWidth,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['aside'], keyof Own> = $props();

  const folded = $derived(collapsible && collapsed);
  const classes = $derived(
    [
      'bb-rail',
      collapsible ? 'bb-rail--collapsible' : null,
      folded ? 'bb-rail--collapsed' : null,
      alwaysVisible ? 'bb-rail--always-visible' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
  const toggleLabel = $derived(i18n.t(folded ? 'nav.railExpand' : 'nav.railCollapse'));
  const insetStyle = $derived(
    [
      topInset !== undefined ? `--rail-top-inset:${topInset}px` : null,
      collapsedWidth !== undefined ? `--rail-collapsed-width:${collapsedWidth}px` : null,
    ]
      .filter(Boolean)
      .join(';') || undefined,
  );

  function handleNav(event: MouseEvent) {
    const link = (event.target as Element).closest<HTMLAnchorElement>('a[href]');
    if (!link) return;
    accountOpen = false;
    if (!onnavigate) return;
    const href = link.getAttribute('href');
    if (!href) return;
    event.preventDefault();
    onnavigate(href);
  }

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
    return mountGlide(railEl, { observe: navEl, selector: '.bb-rail-item.is-active' });
  });
</script>

<aside
  bind:this={railEl}
  class={classes}
  aria-label={label}
  style={insetStyle}
  {...rest}
  ><Brand
    title={brand.title}
    sub={brand.sub}
    href={brand.href}
    logoSrc={brand.logoSrc}
    logoAlt={brand.logoAlt}
    logoShape={brand.logoShape}
    class="bb-cursor-quiet"
  /><Glide /><!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div bind:this={navEl} class="bb-rail__nav" onclick={handleNav}
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
                title={folded ? item.label : undefined}
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
            ><div class={['bb-rail__subs', isOpen(item) ? 'is-open' : null].filter(Boolean).join(' ')}
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
              title={folded ? item.label : undefined}
            />{/if}{/each}</div
      >{/each}</div
  ><div class="bb-rail__spacer"></div>{#if account}<ProfileMenu
      variant="rail"
      bind:open={accountOpen}
      name={account.name}
      caption={account.caption}
      links={account.links ?? []}
      linksLabel={account.linksLabel}
      exit={account.exit}
      help={account.help ?? []}
      helpLabel={account.helpLabel}
      helpTitle={account.helpTitle}
      feedback={account.feedback}
      logoutLabel={account.logoutLabel}
      logoutAction={account.logoutAction}
      onLogout={account.onLogout}
      menuLabel={account.menuLabel}
      helpInAccount={folded}
      >{#snippet avatar(a)}<Bolota name={a.name} size={a.size} active={a.active} gate={a.item} plate={false} />{/snippet}</ProfileMenu
    >{:else if footer}{@render footer()}{/if}{#if collapsible}<button
      type="button"
      class="bb-rail-item bb-rail__toggle"
      aria-expanded={!folded}
      aria-label={toggleLabel}
      title={folded ? toggleLabel : undefined}
      onclick={() => (collapsed = !collapsed)}
      ><Icon name="sidebar" /><span class="bb-rail-item__label">{i18n.t('nav.railCollapse')}</span></button
    >{/if}</aside
>
