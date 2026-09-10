<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // The instrument-panel light: a tan->green arc parked off the right edge
  // plus a green sheen washing in from the top-right corner. Geometry is
  // ../styles/card-atmosphere.css; where the layer sits and when it brightens
  // belong to the card that hosts it and live in elements/card.css.
  //
  // Rendered by Card when `atmo` is set. Exported on its own as well because
  // one surface (the changelog) draws its own card shape and still wants the
  // same material inside it.
  import '../styles/elements/card.css';
</script>

<div class="bb-card-atmo" aria-hidden="true">
  <span class="bb-card-atmo__ring"></span>
  <span class="bb-card-atmo__sheen"></span>
</div>
