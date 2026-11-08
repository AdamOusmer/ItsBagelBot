<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/donate-card.css';
  import ButtonLink from './ButtonLink.svelte';
  import HaloPanel from './HaloPanel.svelte';

  type Own = {
    compact?: boolean;
    label?: string;
    title: string;
    text?: string;
    cta: string;
    href: string;
    note?: string;
    class?: string;
    children?: Snippet;
  };

  let {
    compact = false,
    label,
    title,
    text,
    cta,
    href,
    note,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(['bb-donate-card', compact && 'bb-donate-card--compact', className || null].filter(Boolean).join(' '));
</script>

{#if compact}
<div class={classes} {...rest}>
  <div class="bb-donate-card__crowd" aria-hidden="true">{@render children?.()}</div>
  <HaloPanel as="section" tone="green" class="bb-donate-card__panel">
    <h2 class="bb-donate-card__title">{title}</h2>
    <ButtonLink class="bb-donate-card__cta" size="sm" {href} target="_blank" rel="noopener noreferrer">{cta}</ButtonLink>
  </HaloPanel>
</div>
{:else}
<section class={classes} {...rest}><div class="bb-donate-card__crowd" aria-hidden="true">{@render children?.()}</div><span class="bb-donate-card__label"><bb-fit>{label}</bb-fit></span><h2 class="bb-donate-card__title"><bb-fit mode="block">{title}</bb-fit></h2><p class="bb-donate-card__text"><bb-fit mode="block">{text}</bb-fit></p><ButtonLink class="bb-donate-card__cta" {href} target="_blank" rel="noopener noreferrer"><bb-fit>{cta}</bb-fit></ButtonLink><span class="bb-donate-card__note"><bb-fit>{note}</bb-fit></span></section>
{/if}
