<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/link-row.css';
  import Heading from './Heading.svelte';
  import TextLink from './TextLink.svelte';

  type Own = {
    title?: string;
    links: { href: string; label: string; external?: boolean }[];
    class?: string;
  };

  let {
    title,
    links,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['nav'], keyof Own> = $props();

  const classes = $derived(['bb-link-row', className || null].filter(Boolean).join(' '));
</script>

<nav class={classes} aria-label={title} {...rest}>{#if title}<Heading level={5} as="h2" class="bb-link-row__title"><span data-fit>{title}</span></Heading>{/if}<ul class="bb-link-row__list">{#each links as link (link.href)}<li><TextLink href={link.href} label={link.label} external={link.external} fit /></li>{/each}</ul></nav>
