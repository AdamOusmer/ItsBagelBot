<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/donate-card.css';
  import ButtonLink from './ButtonLink.svelte';

  type Own = {
    label: string;
    title: string;
    text: string;
    cta: string;
    href: string;
    note: string;
    class?: string;
    children?: Snippet;
  };

  let {
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

  const classes = $derived(['bb-donate-card', className || null].filter(Boolean).join(' '));
</script>

<section class={classes} {...rest}><div class="bb-donate-card__crowd" aria-hidden="true">{@render children?.()}</div><span class="bb-donate-card__label"><span data-fit>{label}</span></span><h2 class="bb-donate-card__title"><span data-fit="block">{title}</span></h2><p class="bb-donate-card__text"><span data-fit="block">{text}</span></p><ButtonLink class="bb-donate-card__cta" {href} target="_blank" rel="noopener noreferrer"><span data-fit>{cta}</span></ButtonLink><span class="bb-donate-card__note"><span data-fit>{note}</span></span></section>
