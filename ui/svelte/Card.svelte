<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import type { Snippet } from 'svelte';
  import CardAtmosphere from './CardAtmosphere.svelte';
  import '../styles/elements/card.css';

  let {
    as = 'div',
    href,
    atmo = false,
    sheen = false,
    stat = false,
    hover = false,
    label = '',
    band,
    class: cls = '',
    children,
    ...rest
  }: {
    as?: string;
    href?: string;
    atmo?: boolean;
    sheen?: boolean;
    stat?: boolean;
    hover?: boolean;
    label?: string;
    band?: Snippet;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-card',
      band && 'bb-card--band',
      stat && 'bb-card--stat',
      sheen && 'bb-card--sheen',
      cls,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element
  this={as}
  class={classes}
  {...href ? { href } : {}}
  data-card=""
  data-atmo={atmo ? '' : undefined}
  data-hover={hover ? '' : undefined}
  {...rest}
>
  {#if band}<span class="bb-card__band">{#if atmo}<CardAtmosphere />{/if}{#if label}<span class="bb-card__label">{label}</span>{/if}<span class="bb-card__band-inner">{@render band()}</span></span><span class="bb-card__body">{@render children()}</span>{:else}{#if atmo}<CardAtmosphere />{/if}{#if label}<span class="bb-card__label">{label}</span>{/if}{@render children()}{/if}</svelte:element>
