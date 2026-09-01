<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Shared surface card, ported from the marketing site's Card.astro: solid
  // warm ink, tan hairline. Radius is 8px here, not the marketing site's 16px:
  // f453b3a2 standardized console radii on 8px.
  //
  // The shell is the marketing site's instrument panel (QuietWork Cards,
  // variant 3a). Two shapes:
  //  - Flat: one plate; `atmosphere` opts into the arc + sheen light.
  //  - Banded: pass a `band` snippet and the card grows a housing — a darker
  //    page-ink band on top holding the visual, with the atmosphere and the
  //    mono `label` inset in its corner; `children` render below on the card
  //    plate inside a padded body. Banded cards wrap children in a body box,
  //    so flex-on-root cards (loyalty's .status-row, the importer's .stepper)
  //    must stay flat.
  //
  // `atmosphere` is OFF by default. It belongs to the PUBLIC surfaces only —
  // the stats page, channel leaderboards, /user/[channel]. Signed-in
  // dashboard pages stack many cards per screen and the light reads as noise
  // there, so they stay flat ink. Do not enable it inside (app).
  //
  // `hover` keeps the interactive language — border brighten, small lift, and
  // the atmosphere's opacity bump. It is opt-in because the console stacks
  // dense non-interactive panels that must not react to a passing cursor.
  //
  // The pointer-tracked tan spotlight this used to carry is gone; the marketing
  // Card dropped it for the atmosphere, and keeping it here would have left the
  // two sites with different hover languages.
  //
  // `as` picks the tag so a card can stay a landmark. Pages that need a
  // <section> with aria-labelledby must pass as="section" — rendering it as a
  // bare <div> silently drops the labelled region from the a11y tree.
  import type { Snippet } from 'svelte';
  import CardAtmosphere from './CardAtmosphere.svelte';
  let {
    as = 'div',
    atmosphere = false,
    sheen = false,
    stat = false,
    hover = false,
    label = '',
    band,
    class: cls = '',
    children,
    ...rest
  }: { as?: string; atmosphere?: boolean; sheen?: boolean; stat?: boolean; hover?: boolean; label?: string; band?: Snippet; class?: string; children: Snippet; [key: string]: unknown } = $props();
</script>

<svelte:element
  this={as}
  class="card {atmosphere ? 'atmo' : ''} {sheen ? 'sheen' : ''} {stat ? 'stat' : ''} {hover ? 'hoverable' : ''} {band ? 'banded' : ''} {cls}"
  {...rest}
>
  {#if band}
    <span class="card__band">
      {#if atmosphere}<CardAtmosphere />{/if}
      {#if label}<span class="card__label">{label}</span>{/if}
      <span class="card__band-inner">{@render band()}</span>
    </span>
    <span class="card__body">{@render children()}</span>
  {:else}
    {#if atmosphere}<CardAtmosphere />{/if}
    {#if label}<span class="card__label">{label}</span>{/if}
    {@render children()}
  {/if}
</svelte:element>

<style>
  .card {
    background: var(--bb-card-bg);
    border: 1px solid var(--bb-border);
    border-radius: 8px;
    padding: var(--card-pad);
    position: relative;
    overflow: hidden;
    transition:
      border-color 360ms var(--bb-ease-out-expo),
      transform 360ms var(--bb-ease-out-expo);
  }
  /* Stacking context only where it is needed: it keeps the atmosphere's
     z-index:-1 between this card's background and its content instead of
     letting it escape to the page. Flat cards must not create one — an
     inspector's sticky panel and the overlay stack sit above them. Banded
     cards isolate the BAND instead, so the housing ink paints under the
     light but the card root stays context-free. */
  .card.atmo:not(.banded) { isolation: isolate; }
  .card.sheen::before { content: ""; position: absolute; inset: 0; pointer-events: none; border-radius: inherit;
    background: radial-gradient(circle at 88% 0%, var(--glow-green, rgba(82,183,136,0.16)), transparent 50%); }
  .card.stat { padding: calc(20px * var(--d)); }

  .card.banded {
    display: flex;
    flex-direction: column;
    padding: 0;
  }

  /* 3a housing: the band is a fixture the visual sits in — page-ink
     background, hairline seam, atmosphere contained.

     A FIXED height, never a min-height: banded cards sit side by side in a
     grid, and a floor lets one card whose head runs long (a wrapped board
     note, a two-line module title) push its own seam down while its
     neighbours stay at the floor. Grids set --card-band-h once, sized to
     their tallest head, so every seam in the row lands on the same line. */
  .card__band {
    position: relative;
    isolation: isolate;
    flex: none;
    display: flex;
    align-items: center;
    height: var(--card-band-h, calc(96px * var(--d, 1)));
    padding: calc(14px * var(--d, 1)) calc(18px * var(--d, 1));
    background: var(--bb-bg-0, #0a0a0a);
    border-bottom: 1px solid rgba(201, 168, 124, 0.12);
    overflow: hidden;
  }

  .card__band-inner {
    position: relative;
    display: block;
    width: 100%;
    height: 100%;
  }

  /* Inside the housing the arc re-parks to band scale: centered on the
     band's midline, smaller, a touch brighter than the card-scale rest. */
  .card__band :global(.card-atmo__ring) {
    top: 50%;
    right: -16%;
    width: 62%;
    --card-atmo-ring: 0.18;
  }

  /* Channel label inset in the housing corner (or the plate corner on a
     flat card that asks for one). */
  .card__label {
    position: absolute;
    top: 10px;
    right: 14px;
    z-index: 1;
    font-family: var(--bb-font-mono, "DM Mono", monospace);
    font-size: 0.6rem;
    letter-spacing: 0.2em;
    text-transform: uppercase;
    white-space: nowrap;
    color: rgba(201, 168, 124, 0.4);
  }

  /* :where() so this scores 0,1,0 and a page can restyle the body it was
     handed. Written as `.card.banded > .card__body` it scored 0,4,0 and beat
     every page's own `.tiles .card__body` (0,3,0): the stats tiles asked for
     a flex column with a gap, silently got this `display: block` instead, and
     their gap stopped existing — which is how the counter's rate line ended
     up printed through the digits. The defaults below stay defaults. */
  :where(.card.banded) > :where(.card__body) {
    position: relative;
    display: block;
    flex: 1;
    padding: var(--card-pad);
  }

  @media (hover: hover) and (pointer: fine) {
    .card.hoverable:hover {
      border-color: rgba(201, 168, 124, 0.38);
      transform: translateY(-3px);
    }
  }
</style>
