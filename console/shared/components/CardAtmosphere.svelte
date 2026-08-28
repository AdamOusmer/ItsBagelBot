<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Card atmosphere — a pocket of the header's light, not a cursor tracker.
  // Ported from the marketing site's CardAtmosphere.astro so console surfaces
  // read as the same material as itsbagelbot.dev.
  //
  // Same discs as the hero (green top-right, tan bottom-left, a tan->green arc)
  // at card scale. They stay static: a `filter: blur` on a still layer is one
  // paint and a cached bitmap. The hitch upstream was animating them — a
  // ::before that pulsed scale(1.1) on every orb plus a masked ring spinning on
  // every tile, which re-rasters the blur at 60fps. Hover only changes opacity
  // (compositor), never transform or filter.
  //
  // Divergence from the .astro original: it stacks content above the orbs with
  // a `.card__body` wrapper at z-index 1. Console cards put layout on the card
  // ROOT (loyalty's .status-row and the importer's .stepper are display:flex
  // cards), and a wrapper element would swallow their children out of the flex
  // container. So the orbs go to z-index:-1 under Card's `isolation: isolate`
  // instead: a negative-z child paints after the card's own background but
  // before in-flow content, which is the same result with no extra box.
</script>

<div class="card-atmo" aria-hidden="true">
  <span class="card-atmo__ring"></span>
  <span class="card-atmo__orb card-atmo__orb--green"></span>
  <span class="card-atmo__orb card-atmo__orb--tan"></span>
</div>

<style>
  .card-atmo {
    position: absolute;
    inset: 0;
    z-index: -1;
    overflow: hidden;
    pointer-events: none;
    border-radius: inherit;
    /* Header orbs rest at ~0.16 on a full viewport; cards are smaller so the
       same number reads as nothing. 0.28 at rest / 0.42 on hover was measured
       upstream against --bb-card-bg #111110 (same ink here) after the pulsed
       ::before came off: 0.22 (the pulsed rest) went flat without that layer. */
    --card-atmo-orb: 0.28;
  }

  /* Even siblings flip so neighbouring cards don't share one corner glow. */
  :global(.card:nth-child(even)) .card-atmo { transform: scaleX(-1); }

  /* Only interactive cards brighten. Console stacks dense non-interactive
     panels, so an unconditional :hover would make the whole page twitch. */
  :global(.card.hoverable:hover) .card-atmo,
  :global(.card.hoverable:focus-visible) .card-atmo {
    --card-atmo-orb: 0.42;
  }

  /* Mini of the header's ring: a 1.5px tan->green arc, not a filled disc.
     Masked conic so we never mint per-card SVG ids (g1/g2 would collide).
     Parked half off the right edge the way the hero ring sits off-canvas.
     No spin — rotating a masked layer on every tile was the other hitch. */
  .card-atmo__ring {
    position: absolute;
    top: 42%;
    right: -22%;
    width: 92%;
    aspect-ratio: 1;
    translate: 0 -50%;
    border-radius: 50%;
    opacity: 0.16;
    background: conic-gradient(
      from 210deg,
      rgba(201, 168, 124, 0.9),
      rgba(45, 106, 79, 0.14) 30%,
      rgba(64, 145, 108, 0.75) 55%,
      rgba(201, 168, 124, 0.2) 80%,
      rgba(201, 168, 124, 0.9)
    );
    -webkit-mask: radial-gradient(farthest-side, transparent calc(100% - 1.6px), #000 calc(100% - 0.4px));
    mask: radial-gradient(farthest-side, transparent calc(100% - 1.6px), #000 calc(100% - 0.4px));
  }

  .card-atmo__orb {
    position: absolute;
    border-radius: 50%;
    filter: blur(42px);
    opacity: var(--card-atmo-orb);
    transition: opacity 360ms var(--bb-ease-out-expo, cubic-bezier(0.16, 1, 0.3, 1));
  }

  .card-atmo__orb--green {
    width: 240px;
    height: 240px;
    background-color: var(--bb-green, #2d6a4f);
    top: -48px;
    right: -36px;
  }

  .card-atmo__orb--tan {
    width: 200px;
    height: 200px;
    background-color: var(--bb-tan, #c9a87c);
    bottom: -40px;
    left: -32px;
  }
</style>
