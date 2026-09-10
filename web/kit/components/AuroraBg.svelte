<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Ambient motion background for the onboarding. Pure CSS: a few blurred brand
  // orbs drifting on independent loops. No JS, no conic mesh, modest blur: keeps
  // it cheap so login/logout stay smooth. Freezes for reduced-motion.
</script>

<div class="aurora" aria-hidden="true">
  <span class="bb-orb bb-orb--aurora-green bb-orb--drift-1 orb o1"></span>
  <span class="bb-orb bb-orb--aurora-tan bb-orb--drift-2 orb o2"></span>
  <span class="bb-orb bb-orb--aurora-green-soft bb-orb--drift-3 orb o3"></span>
</div>

<style>
  .aurora {
    position: fixed;
    inset: 0;
    z-index: 0;
    overflow: hidden;
    pointer-events: none;
    background: var(--bb-black, #0a0a0a);
  }
  /* Shape, blur, the three wash gradients and the three drift loops are
     @bagel/ui/styles/orbs.css; the rgba literals this file wrote out are
     --bb-green-glow-rgb, --bb-green-rgb and --bb-tan-rgb, and the drift
     periods (24s / 30s / 26s) are its keyframes. Placement, size and the
     per-orb opacity stay here: this backdrop is composed for the login
     screen and nothing else renders the three together. */
  .orb {
    --bb-orb-blur: 56px;
  }
  .o1 {
    top: -10%;
    left: 6%;
    width: min(560px, 60vw);
    aspect-ratio: 1;
    --bb-orb-opacity: 0.55;
  }
  .o2 {
    bottom: -14%;
    right: 4%;
    width: min(480px, 54vw);
    aspect-ratio: 1;
    --bb-orb-opacity: 0.5;
  }
  .o3 {
    top: 34%;
    right: 26%;
    width: min(360px, 40vw);
    aspect-ratio: 1;
    --bb-orb-opacity: 0.4;
  }
</style>
