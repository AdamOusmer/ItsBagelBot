<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/pager.css';
  import ButtonLink from './ButtonLink.svelte';

  type Own = {
    label: string;
    prevHref: string;
    nextHref: string;
    hasPrev?: boolean;
    hasNext?: boolean;
    prevLabel?: string;
    nextLabel?: string;
    class?: string;
  };

  let {
    label,
    prevHref,
    nextHref,
    hasPrev = true,
    hasNext = true,
    prevLabel = 'Previous',
    nextLabel = 'Next',
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const classes = $derived(['bb-pager', className || null].filter(Boolean).join(' '));
</script>

<div class={classes} {...rest}><ButtonLink variant="ghost" href={prevHref} disabled={!hasPrev}>{prevLabel}</ButtonLink><span class="bb-pager__label">{label}</span><ButtonLink variant="ghost" href={nextHref} disabled={!hasNext}>{nextLabel}</ButtonLink></div>
