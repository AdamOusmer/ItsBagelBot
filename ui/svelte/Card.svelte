<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // The Svelte adapter for the card contract. Its Astro twin is
  // ../astro/Card.astro; ui/test/parity.test.ts holds the two to the same
  // markup. The look is entirely ../styles/elements/card.css.
  //
  // Two shapes:
  //  - Flat: one plate; children render directly on it, so a card whose root
  //    carries the layout (loyalty's .status-row, the importer's .stepper are
  //    display:flex cards) keeps working.
  //  - Banded: pass a `band` snippet and the card grows a housing -- a darker
  //    page-ink band on top holding the visual, with the atmosphere and the
  //    mono `label` inset in its corner; `children` render below on the card
  //    plate inside a padded body box.
  //
  // `atmo` and `hover` are BOTH opt-in, which is the console's default rather
  // than the marketing site's (divergences C1/C2). The site's cards are links
  // and every one of them wants both; the console stacks dense
  // non-interactive panels per screen, where the light reads as noise and a
  // lift under a passing cursor makes the page twitch. Defaults that are wrong
  // for one surface have to be wrong for the one with fewer call sites, and
  // the site passes both explicitly.
  //
  // `as` picks the tag so a card can stay a landmark. A page that needs a
  // <section> with aria-labelledby must pass as="section": rendering it as a
  // bare <div> silently drops the labelled region from the a11y tree.
  import type { Snippet } from 'svelte';
  import CardAtmosphere from './CardAtmosphere.svelte';
  import '../styles/elements/card.css';

  // `href` is spread conditionally rather than written as `{href}`: Svelte's
  // <svelte:element> types its attributes as HTMLAttributes, which has no
  // `href` (it belongs to <a>), and an unconditional `href={undefined}` would
  // also have to render in the same slot as the Astro twin's to keep the
  // parity diff quiet.
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

  // Same list, same order, same join as the Astro adapter.
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
  <!-- Written without whitespace between the blocks: a newline between
       `{/if}` and `{@render children()}` is a text node, and it reaches the DOM
       as a leading space inside the card. -->
  {#if band}<span class="bb-card__band">{#if atmo}<CardAtmosphere />{/if}{#if label}<span class="bb-card__label">{label}</span>{/if}<span class="bb-card__band-inner">{@render band()}</span></span><span class="bb-card__body">{@render children()}</span>{:else}{#if atmo}<CardAtmosphere />{/if}{#if label}<span class="bb-card__label">{label}</span>{/if}{@render children()}{/if}</svelte:element>
