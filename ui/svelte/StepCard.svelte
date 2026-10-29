<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import type { Snippet } from 'svelte';
  import '../styles/elements/step-card.css';
  import Card from './Card.svelte';
  import Eyebrow from './Eyebrow.svelte';
  import Heading from './Heading.svelte';
  import Text from './Text.svelte';

  type Own = {
    index: number;
    title?: string;
    class?: string;
    children: Snippet;
  };

  let {
    index,
    title,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const classes = $derived(['bb-step-card', className || null].filter(Boolean).join(' '));
</script>

<li><Card atmo hover class={classes} {...rest}><Eyebrow aria-hidden="true">{String(index).padStart(2, '0')}</Eyebrow>{#if title}<Heading level={5} as="h3" class="bb-step-card__title"><span data-fit="block">{title}</span></Heading>{/if}<Text size="sm" tone="muted" class="bb-step-card__body"><span data-fit="block">{@render children()}</span></Text></Card></li>
