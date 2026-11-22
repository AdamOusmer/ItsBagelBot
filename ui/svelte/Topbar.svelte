<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/shell.css';
  import { getUiI18n } from './i18n';
  import Brand from './Brand.svelte';
  import Bolota from './Bolota.svelte';
  import Button from './Button.svelte';
  import ButtonLink from './ButtonLink.svelte';
  import { mountClock } from '../lib/clock';
  import { topbarClasses, topbarCrumbs } from '../lib/topbar-context';
  import type { Snippet } from 'svelte';
  import type { UiBrand, UiContextAction, UiCrumb, UiTopbarContext } from '../lib/nav-types';

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

  let clockEl = $state<HTMLElement | null>(null);
  $effect(() => {
    if (!clockEl || !clock) return;
    return mountClock(clockEl);
  });
</script>

{#snippet actionLabel(item: UiContextAction)}<span class="bb-topbar-ctx__label">{item.label}</span
  >{#if item.compactLabel}<span class="bb-topbar-ctx__compact">{item.compactLabel}</span>{/if}{/snippet}

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
  />{#if context}<span class="bb-topbar-ctx__title" aria-hidden="true">{context.title}</span
      ><div class="bb-topbar-ctx" role="status" aria-label={context.statusText} title={context.scopesTitle}
      >{#if context.subject}<span class="bb-topbar-ctx__avatar" aria-hidden="true"
          ><Bolota name={context.subject} size={36} plate={false} /></span
        ><span class="bb-topbar-ctx__text"
          >{#if context.subjectPre}<span class="bb-topbar-ctx__affix">{context.subjectPre}</span
            >{/if}<strong>{context.subject}</strong
          >{#if context.subjectPost}<span class="bb-topbar-ctx__affix">{context.subjectPost}</span
            >{/if}</span
        >{/if}</div
    >{/if}{#if shownCrumbs.length}<nav
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
            class={item.subtle ? 'bb-topbar-ctx__subtle' : undefined}
            ><Button type="submit" variant="secondary" size="sm">{@render actionLabel(item)}</Button></form
          >{:else}<ButtonLink
            href={item.href ?? '/'}
            variant="secondary"
            size="sm"
            class={item.subtle ? 'bb-topbar-ctx__subtle' : undefined}>{@render actionLabel(item)}</ButtonLink
          >{/if}{/each}</div
    >{/if}</header
>
