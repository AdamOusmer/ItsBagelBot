<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Card atmosphere — the instrument-panel light (QuietWork Cards, variant
  // 3a), ported from the marketing site's CardAtmosphere.astro so console
  // surfaces read as the same material as itsbagelbot.dev: the hero's
  // tan->green arc parked off the right edge plus a green sheen washing in
  // from the top-right corner. The blurred green/tan orbs the earlier
  // atmosphere carried are gone — they were two 42px-blur bitmaps per card;
  // the sheen is one unblurred radial gradient. Character from structure,
  // not from more glow.
  //
  // Everything stays static: the arc never spins (rotating a masked layer on
  // every tile re-rasters at 60fps — the original hitch) and hover only
  // nudges opacity, which stays on the compositor.
  //
  // Divergence from the .astro original: console cards put layout on the card
  // ROOT (loyalty's .status-row and the importer's .stepper are display:flex
  // cards), and a wrapper element would swallow their children out of the flex
  // container. So the layers go to z-index:-1 under Card's `isolation:
  // isolate` instead: a negative-z child paints after the card's own
  // background but before in-flow content, which is the same result with no
  // extra box.
</script>

<div class="card-atmo" aria-hidden="true">
  <span class="card-atmo__ring"></span>
  <span class="card-atmo__sheen"></span>
</div>

<style>
  .card-atmo {
    position: absolute;
    inset: 0;
    z-index: -1;
    overflow: hidden;
    pointer-events: none;
    border-radius: inherit;
    /* Rest/hover pair replaces the old orb bump (0.28->0.42): the sheen's
       0.12 alpha is authored into the gradient, so the var scales it — 1.6
       lands the hover wash at ~0.19 against the card ink. */
    --card-atmo-glow: 1;
    --card-atmo-ring: 0.16;
  }

  /* Even siblings flip so neighbouring cards don't share one corner glow. */
  :global(.card:nth-child(even)) .card-atmo { transform: scaleX(-1); }

  /* Only interactive cards brighten. Console stacks dense non-interactive
     panels, so an unconditional :hover would make the whole page twitch. */
  :global(.card.hoverable:hover) .card-atmo,
  :global(.card.hoverable:focus-visible) .card-atmo {
    --card-atmo-glow: 1.6;
    --card-atmo-ring: 0.24;
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
    opacity: var(--card-atmo-ring);
    transition: opacity 360ms var(--bb-ease-out-expo, cubic-bezier(0.16, 1, 0.3, 1));
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

  .card-atmo__sheen {
    position: absolute;
    inset: 0;
    opacity: var(--card-atmo-glow);
    transition: opacity 360ms var(--bb-ease-out-expo, cubic-bezier(0.16, 1, 0.3, 1));
    background: radial-gradient(120% 80% at 100% 0%, rgba(82, 183, 136, 0.12), transparent 60%);
  }
</style>
