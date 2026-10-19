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
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['nav'], keyof Own> = $props();

  const classes = $derived(['bb-pager', className || null].filter(Boolean).join(' '));
</script>

<nav class={classes} aria-label={navLabel} {...rest}><ButtonLink variant="ghost" href={prevHref} disabled={!hasPrev}>{prevLabel}</ButtonLink><span class="bb-pager__label" aria-current="page">{#if fit}<span data-fit="">{label}</span>{:else}{label}{/if}</span><ButtonLink variant="ghost" href={nextHref} disabled={!hasNext}>{nextLabel}</ButtonLink></nav>
