<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/section-heading.css';
  import type { Snippet } from 'svelte';

  type Own = {
    eyebrow?: string;
    title: string;
    align?: 'center' | 'left';
    class?: string;
    badge?: Snippet;
  };

  let {
    eyebrow,
    title,
    align = 'center',
    class: className = '',
    badge,
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const classes = $derived(
    ['bb-section-heading', `bb-section-heading--${align}`, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div class={classes} {...rest}>{#if eyebrow || badge}<div
    class="bb-section-heading__meta"
    data-reveal
  >{#if eyebrow}<span class="bb-section-heading__eyebrow">{eyebrow}</span>{/if}{#if badge}{@render badge()}{/if}</div
  >{/if}<h2 class="bb-section-heading__title" data-reveal style="--reveal-i: 1">{title}</h2></div>
