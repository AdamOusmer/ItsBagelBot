<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Shared surface card, ported from the marketing site's Card.astro: solid
  // warm ink, tan hairline. Radius is 8px here, not the marketing site's 16px:
  // f453b3a2 standardized console radii on 8px.
  //
  // `atmosphere` opts a card into the header's orb/ring light at card scale
  // (see CardAtmosphere) and is OFF by default. It belongs to the PUBLIC
  // surfaces only — the stats page, channel leaderboards, /user/[channel].
  // Signed-in dashboard pages stack many cards per screen and the orbs read as
  // noise there, so they stay flat ink. Do not enable it inside (app).
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
    class: cls = '',
    children,
    ...rest
  }: { as?: string; atmosphere?: boolean; sheen?: boolean; stat?: boolean; hover?: boolean; class?: string; children: Snippet; [key: string]: unknown } = $props();
</script>

<svelte:element
  this={as}
  class="card {atmosphere ? 'atmo' : ''} {sheen ? 'sheen' : ''} {stat ? 'stat' : ''} {hover ? 'hoverable' : ''} {cls}"
  {...rest}
>
  {#if atmosphere}<CardAtmosphere />{/if}
  {@render children()}
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
     inspector's sticky panel and the overlay stack sit above them. */
  .card.atmo { isolation: isolate; }
  .card.sheen::before { content: ""; position: absolute; inset: 0; pointer-events: none; border-radius: inherit;
    background: radial-gradient(circle at 88% 0%, var(--glow-green, rgba(82,183,136,0.16)), transparent 50%); }
  .card.stat { padding: calc(20px * var(--d)); }

  @media (hover: hover) and (pointer: fine) {
    .card.hoverable:hover {
      border-color: rgba(201, 168, 124, 0.38);
      transform: translateY(-3px);
    }
  }
</style>
