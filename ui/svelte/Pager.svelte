<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/pager.css';
  import { getUiI18n } from './i18n';
  import ButtonLink from './ButtonLink.svelte';

  const i18n = getUiI18n();
  type Own = {
    label: string;
    prevHref: string;
    nextHref: string;
    hasPrev?: boolean;
    hasNext?: boolean;
    prevLabel?: string;
    nextLabel?: string;
    navLabel?: string;
    fit?: boolean;
    fixed?: boolean;
    class?: string;
  };

  let {
    label,
    prevHref,
    nextHref,
    hasPrev = true,
    hasNext = true,
    prevLabel = i18n.t('nav.previous'),
    nextLabel = i18n.t('nav.next'),
    navLabel = i18n.t('nav.pagination'),
    fit = false,
    fixed = false,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['nav'], keyof Own> = $props();

  const classes = $derived(['bb-pager', fixed ? 'bb-pager--fixed' : null, className || null].filter(Boolean).join(' '));
</script>

<nav class={classes} aria-label={navLabel} {...rest}><ButtonLink variant="secondary" href={prevHref} disabled={!hasPrev}>{#if fixed}<span data-fit="">{prevLabel}</span>{:else}{prevLabel}{/if}</ButtonLink><span class="bb-pager__label" aria-current="page">{#if fit}<span data-fit="">{label}</span>{:else}{label}{/if}</span><ButtonLink variant="secondary" href={nextHref} disabled={!hasNext}>{#if fixed}<span data-fit="">{nextLabel}</span>{:else}{nextLabel}{/if}</ButtonLink></nav>
