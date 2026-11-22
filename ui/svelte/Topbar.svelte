<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/shell.css';
  import { getUiI18n } from './i18n';
  import Brand from './Brand.svelte';
  import Icon from './Icon.svelte';
  import { mountClock } from '../lib/clock';
  import { contextActionClass, topbarClasses, topbarCrumbs, visibleScopes } from '../lib/topbar-context';
  import type { Snippet } from 'svelte';
  import type { UiBrand, UiCrumb, UiTopbarContext } from '../lib/nav-types';

  const i18n = getUiI18n();
  type Own = {
    brand: UiBrand;
    crumbs?: UiCrumb[];
    crumbLabel?: string;
    clock?: boolean;
    clockText?: string;
    railed?: boolean;
    context?: UiTopbarContext;
    actions?: Snippet;
    account?: Snippet;
    class?: string;
  };

  let {
    brand,
    crumbs = [],
    crumbLabel = i18n.t('nav.breadcrumb'),
    clock = true,
    clockText = '',
    railed = false,
    context,
    actions,
    account,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['header'], keyof Own> = $props();

  const classes = $derived(topbarClasses({ context, className }));
  const shownCrumbs = $derived(topbarCrumbs(crumbs, context));
  const scopes = $derived(visibleScopes(context?.scopes));

  let clockEl = $state<HTMLElement | null>(null);
  $effect(() => {
    if (!clockEl || !clock) return;
    return mountClock(clockEl);
  });
</script>

<header class={classes} {...rest}
  ><Brand
    title={brand.title}
    sub={brand.sub}
    href={brand.href}
    logoSrc={brand.logoSrc}
    logoAlt={brand.logoAlt}
    size="sm"
    logoShape={brand.logoShape}
    style="--bb-cursor: quiet"
  />{#if context}<div class="bb-topbar-ctx" role="status" aria-label={context.statusText}
      >{#each context.badges as badge (badge.label)}<span
          class={`bb-topbar-ctx__badge bb-topbar-ctx__badge--${badge.tone}`}
          ><Icon name={badge.icon} size={13} strokeWidth={2} /><span>{badge.label}</span></span
        >{/each}{#if context.subject}<span class="bb-topbar-ctx__subject"
          >{#if context.avatar}<span
              class={`bb-topbar-ctx__avatar bb-topbar-ctx__avatar--${context.tone}`}
              aria-hidden="true">{context.avatar}</span
            >{/if}<span class="bb-topbar-ctx__text"
            >{#if context.subjectPre}<span class="bb-topbar-ctx__affix">{context.subjectPre}</span
              >{/if}<strong>{context.subject}</strong
            >{#if context.subjectPost}<span class="bb-topbar-ctx__affix">{context.subjectPost}</span
              >{/if}</span
          ></span
        >{/if}{#if scopes.visible.length}<span class="bb-topbar-ctx__scopes" title={context.scopesTitle}
          >{#each scopes.visible as scope (scope)}<span class="bb-topbar-ctx__scope">{scope}</span
            >{/each}{#if scopes.overflow > 0}<span class="bb-topbar-ctx__scope"
              >+{scopes.overflow}</span
            >{/if}</span
        >{/if}</div
    >{#if shownCrumbs.length}<span class="bb-topbar-ctx__sep" aria-hidden="true"></span>{/if}{/if}{#if shownCrumbs.length}<nav
      class="bb-topbar__crumb"
      aria-label={crumbLabel}
      ><ol
        >{#each shownCrumbs as crumb, i (crumb.label)}<li
            class={i === shownCrumbs.length - 1 ? 'is-current' : undefined}
            >{#if crumb.href}<a href={crumb.href}>{crumb.label}</a>{:else}<span
                aria-current="page">{crumb.label}</span
              >{/if}</li
          >{/each}</ol
      ></nav
    >{/if}<div class="bb-topbar__grow"></div>{#if clock}<span
      bind:this={clockEl}
      class="bb-topbar__clock"
      aria-hidden="true">{clockText}</span
    >{/if}{#if account}<div
      class={['bb-topbar__account', railed && 'bb-topbar__account--railed']}>{@render account()}</div
    >{/if}{#if actions}{@render actions()}{/if}{#if context && context.actions.length}<div
      class="bb-topbar-ctx__actions"
      >{#each context.actions as item (item.label)}{#if item.formAction}<form
            method="POST"
            action={item.formAction}
            ><button type="submit" class={contextActionClass(item, context.tone)}
              ><Icon name="logOut" size={13} strokeWidth={2} /><span class="bb-topbar-ctx__label"
                >{item.label}</span
              >{#if item.compactLabel}<span class="bb-topbar-ctx__compact">{item.compactLabel}</span
                >{/if}</button
            ></form
          >{:else}<a class={contextActionClass(item, context.tone)} href={item.href ?? '/'}
            ><Icon name="logOut" size={13} strokeWidth={2} /><span class="bb-topbar-ctx__label"
              >{item.label}</span
            >{#if item.compactLabel}<span class="bb-topbar-ctx__compact">{item.compactLabel}</span
              >{/if}</a
          >{/if}{/each}</div
    >{/if}</header
>
