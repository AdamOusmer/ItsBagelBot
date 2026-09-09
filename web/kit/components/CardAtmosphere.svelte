<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Card atmosphere: the instrument-panel light (QuietWork Cards, variant 3a).
  // The ring and sheen geometry is shared verbatim with the marketing site's
  // CardAtmosphere.astro (@bagel/ui/styles/card-atmosphere.css) so console surfaces
  // read as the same material as itsbagelbot.com instead of as a port that
  // slowly stops matching.
  //
  // Two divergences from the site, and they are why the shared part is CSS
  // rather than one component:
  //
  // 1. Stacking. Console cards put layout on the card ROOT (loyalty's
  //    .status-row and the importer's .stepper are display:flex cards), and a
  //    wrapper element would swallow their children out of the flex container.
  //    So the layers go to z-index:-1 under Card's `isolation: isolate`: a
  //    negative-z child paints after the card's own background but before
  //    in-flow content, which is the same result with no extra box.
  // 2. Hover. Only interactive cards brighten. The console stacks dense
  //    non-interactive panels, so the site's unconditional :hover would make
  //    the whole page twitch.
  import '@bagel/ui/styles/card-atmosphere.css';
</script>

<div class="card-atmo" aria-hidden="true">
  <span class="card-atmo__ring"></span>
  <span class="card-atmo__sheen"></span>
</div>

<style>
  .card-atmo {
    z-index: -1;
    border-radius: inherit;
    --card-atmo-ease: var(--bb-ease-out-expo, cubic-bezier(0.16, 1, 0.3, 1));
  }

  /* Even siblings flip so neighbouring cards don't share one corner glow. */
  :global(.card:nth-child(even)) .card-atmo { transform: scaleX(-1); }

  :global(.card.hoverable:hover) .card-atmo,
  :global(.card.hoverable:focus-visible) .card-atmo {
    --card-atmo-glow: 1.6;
    --card-atmo-ring: 0.24;
  }
</style>
