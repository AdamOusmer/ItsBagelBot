<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // The onboarding atmosphere, shared by pages that need the same depth.
  // Props describe stage/pointer motion; copy and foreground artwork stay in the app.
  import LightField from './LightField.svelte';
  import '../styles/orbs.css';
  import '../styles/elements/ambient-sky.css';

  const generatedId = $props.id();
  let {
    shift = 0, turn = 0, px = 0, py = 0, progress = 0, leaving = false,
    position = 'fixed', warmth = 0.7, uid = generatedId,
    class: className = '', ...rest
  }: {
    /** Lateral stage offset, -1..1. */
    shift?: number;
    /** Additional ring rotation in degrees. */
    turn?: number;
    /** Pointer position relative to the viewport centre, -1..1. */
    px?: number;
    py?: number;
    /** Horizon warmth, clamped to 0..1. */
    progress?: number;
    leaving?: boolean;
    /** Contained fills a positioned ancestor; fixed fills the viewport. */
    position?: 'contained' | 'fixed';
    warmth?: number;
    /** Override for deterministic rendering; each sky needs a unique value. */
    uid?: string;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-ambient-sky', `bb-ambient-sky--${position}`, leaving && 'bb-ambient-sky--leaving', className].filter(Boolean).join(' '));
  const variables = $derived(`--ambient-shift: ${shift}; --ambient-turn: ${turn}deg; --ambient-px: ${px}; --ambient-py: ${py}; --ambient-progress: ${Math.min(Math.max(progress, 0), 1)};`);
</script>

<div
  class={classes}
  aria-hidden="true"
  style={variables} {...rest}
>
  <div class="bb-ambient-sky__dawn"></div>
  <div class="bb-ambient-sky__field"><LightField {warmth} /></div>
  <div class="bb-ambient-sky__ring">
    <svg viewBox="0 0 800 800" fill="none" xmlns="http://www.w3.org/2000/svg">
      <path
        d="M400 400 Q 550 200 650 400 Q 750 600 550 700 Q 350 800 200 650 Q 50 500 150 300 Q 250 100 450 150 Q 650 200 700 400 Q 750 600 600 720 Q 450 840 280 760 Q 110 680 80 500 Q 50 320 180 200 Q 310 80 480 120"
        stroke={`url(#${uid}-green)`}
        stroke-width="2.5"
        fill="none"
      />
      <path
        d="M400 400 Q 240 220 150 400 Q 60 580 240 680 Q 420 780 560 660 Q 700 540 680 360 Q 660 180 480 140 Q 300 100 180 240 Q 60 380 120 560 Q 180 740 360 780"
        stroke={`url(#${uid}-tan)`}
        stroke-width="1.5"
        fill="none"
      />
      <defs>
        <linearGradient id={`${uid}-green`} x1="0" y1="0" x2="800" y2="800" gradientUnits="userSpaceOnUse">
          <stop stop-color="#c9a87c" offset="0" />
          <stop offset="1" stop-color="#2d6a4f" />
        </linearGradient>
        <linearGradient id={`${uid}-tan`} x1="800" y1="0" x2="0" y2="800" gradientUnits="userSpaceOnUse">
          <stop stop-color="#40916c" offset="0" />
          <stop offset="1" stop-color="#c9a87c" />
        </linearGradient>
      </defs>
    </svg>
  </div>
  <div class="bb-orb bb-orb--halo bb-orb--green bb-ambient-sky__orb bb-ambient-sky__orb--green"></div>
  <div class="bb-orb bb-orb--halo bb-orb--tan bb-ambient-sky__orb bb-ambient-sky__orb--tan"></div>
</div>
