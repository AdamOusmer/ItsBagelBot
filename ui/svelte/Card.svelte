<script lang="ts" generics="T extends keyof SvelteHTMLElements = 'div'">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import type { Snippet } from 'svelte';
  import CardAtmosphere from './CardAtmosphere.svelte';
  import '../styles/elements/card.css';

  type Own = {
    as?: T;
    href?: string;
    atmo?: boolean;
    sheen?: boolean;
    stat?: boolean;
    glass?: boolean;
    hover?: boolean;
    flush?: boolean;
    dashed?: boolean;
    tone?: 'accent' | 'danger';
    label?: string;
    band?: Snippet;
    class?: string;
    children: Snippet;
  };

  let {
    as,
    href,
    atmo = false,
    sheen = false,
    stat = false,
    glass = false,
    hover = false,
    flush = false,
    dashed = false,
    tone,
    label = '',
    band,
    class: cls = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements[T], keyof Own> = $props();

  const classes = $derived(
    [
      'bb-card',
      band && 'bb-card--band',
      stat && 'bb-card--stat',
      sheen && 'bb-card--sheen',
      glass && 'bb-card--glass',
      flush && 'bb-card--flush',
      dashed && 'bb-card--dashed',
      tone && `bb-card--${tone}`,
      cls,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element
  this={as ?? (href ? 'a' : 'div')}
  class={classes}
  {...href ? { href } : {}}
  data-card=""
  data-atmo={atmo ? '' : undefined}
  data-hover={hover ? '' : undefined}
  {...rest}
>
  {#if band}<span class="bb-card__band">{#if atmo}<CardAtmosphere />{/if}{#if label}<span class="bb-card__label">{label}</span>{/if}<span class="bb-card__band-inner">{@render band()}</span></span><span class="bb-card__body">{@render children()}</span>{:else}{#if atmo}<CardAtmosphere />{/if}{#if label}<span class="bb-card__label">{label}</span>{/if}{@render children()}{/if}</svelte:element>
