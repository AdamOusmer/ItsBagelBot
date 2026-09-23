<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // The welcome page's sky: the hero background the marketing site and /login
  // already share (drifting motes, the slow gradient ring, two breathing
  // orbs), plus three things a static hero never needed. The whole sky slides
  // a little against each step change (`shift`), the ring takes an extra turn
  // per step on top of its own spin (`turn`), and every layer leans toward the
  // pointer at its own depth (`px`/`py`), so the field reads as depth rather
  // than as wallpaper. A low horizon glow (`progress`) warms as the journey
  // advances, so how far along you are is felt in the room, not only read
  // off the rail. All of it is transforms on layers that are already
  // composited; nothing here repaints per frame except the motes, which paint
  // their own canvas.
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import '@bagel/ui/styles/orbs.css';

  let {
    shift = 0,
    turn = 0,
    px = 0,
    py = 0,
    progress = 0,
    leaving = false
  }: {
    /** Lateral stage offset, -1..1. The step's side of the screen, in effect. */
    shift?: number;
    /** Extra ring rotation in degrees, on top of its own 40s spin. */
    turn?: number;
    /** Pointer position, -1..1 from the viewport centre. */
    px?: number;
    py?: number;
    /** How far through the journey, 0..1. The horizon warms as it grows. */
    progress?: number;
    /** The exit beat: the sky brightens as the stage leaves. */
    leaving?: boolean;
  } = $props();
</script>

<div
  class="sky"
  class:leaving
  aria-hidden="true"
  style="--shift: {shift}; --turn: {turn}deg; --px: {px}; --py: {py}; --progress: {Math.min(Math.max(progress, 0), 1)};"
>
  <div class="dawn"></div>
  <div class="field"><LightField /></div>
  <div class="ring">
    <svg viewBox="0 0 800 800" fill="none" xmlns="http://www.w3.org/2000/svg">
      <path
        d="M400 400 Q 550 200 650 400 Q 750 600 550 700 Q 350 800 200 650 Q 50 500 150 300 Q 250 100 450 150 Q 650 200 700 400 Q 750 600 600 720 Q 450 840 280 760 Q 110 680 80 500 Q 50 320 180 200 Q 310 80 480 120"
        stroke="url(#wlc-g1)"
        stroke-width="2.5"
        fill="none"
      />
      <path
        d="M400 400 Q 240 220 150 400 Q 60 580 240 680 Q 420 780 560 660 Q 700 540 680 360 Q 660 180 480 140 Q 300 100 180 240 Q 60 380 120 560 Q 180 740 360 780"
        stroke="url(#wlc-g2)"
        stroke-width="1.5"
        fill="none"
      />
      <defs>
        <linearGradient id="wlc-g1" x1="0" y1="0" x2="800" y2="800" gradientUnits="userSpaceOnUse">
          <stop stop-color="#c9a87c" offset="0" />
          <stop offset="1" stop-color="#2d6a4f" />
        </linearGradient>
        <linearGradient id="wlc-g2" x1="800" y1="0" x2="0" y2="800" gradientUnits="userSpaceOnUse">
          <stop stop-color="#40916c" offset="0" />
          <stop offset="1" stop-color="#c9a87c" />
        </linearGradient>
      </defs>
    </svg>
  </div>
  <div class="bb-orb bb-orb--halo bb-orb--green orb orb-1"></div>
  <div class="bb-orb bb-orb--halo bb-orb--tan orb orb-2"></div>
</div>

<style>
  .sky {
    position: fixed;
    inset: 0;
    z-index: 0;
    overflow: hidden;
    pointer-events: none;
  }

  /* One curve, one duration, for every layer: the sky is a single body that
     leans, not four things that each decided how fast to move. 1200ms is
     longer than the copy's own 760ms slide so the background is still
     settling when the foreground has landed, which is what gives it weight. */
  .dawn,
  .field,
  .ring,
  .orb {
    transition:
      transform 1200ms var(--bb-ease-out-expo),
      opacity 600ms var(--bb-ease-out-expo);
    will-change: transform;
  }

  /* Depth is the ratio between these three: the motes barely move, the ring
     moves a little, the orbs move the most, and the field slides the OTHER
     way from the stage so the parallax has a far plane. */
  .field {
    position: absolute;
    inset: 0;
    transform: translate3d(calc(var(--shift) * -1.5vw + var(--px) * 6px), calc(var(--py) * 4px), 0);
  }

  /* Progress made visible in the room itself: a low horizon glow that widens
     and warms as the journey goes on. Opacity and a lateral transform only
     (the house rule is sideways motion); the gradient is painted once. */
  .dawn {
    position: absolute;
    left: -10%;
    right: -10%;
    bottom: -38vh;
    height: 90vh;
    border-radius: 50%;
    background: radial-gradient(
      ellipse at 50% 40%,
      rgba(var(--bb-tan-rgb), 0.22),
      rgba(var(--bb-green-glow-rgb), 0.12) 40%,
      transparent 70%
    );
    filter: blur(30px);
    opacity: calc(0.18 + var(--progress) * 0.72);
    transform: translate3d(calc(var(--shift) * 3vw + var(--px) * 10px), 0, 0) scaleX(calc(0.8 + var(--progress) * 0.3));
    transition:
      transform 1400ms var(--bb-ease-out-expo),
      opacity 1400ms var(--bb-ease-out-expo);
  }

  .ring {
    position: absolute;
    top: 50%;
    right: -180px;
    width: 760px;
    height: 760px;
    opacity: calc(0.08 + var(--progress) * 0.06);
    transform: translate3d(calc(var(--shift) * 4vw + var(--px) * 14px), calc(-50% + var(--py) * 10px), 0)
      rotate(var(--turn));
  }

  .ring svg {
    width: 100%;
    height: 100%;
    animation: slowspin 40s linear infinite;
  }

  /* Shape, blur, pulse and colour are the library's (.bb-orb--halo); where
     each orb sits and how it moves with the stage is this sky's own. */
  .orb {
    --bb-orb-will-change: transform, opacity;
  }

  .orb-1 {
    width: 500px;
    height: 500px;
    top: -120px;
    right: -60px;
    --bb-orb-transform: translate3d(calc(var(--shift) * 6vw + var(--px) * 22px), calc(var(--py) * 16px), 0);
  }

  .orb-2 {
    width: 400px;
    height: 400px;
    bottom: -100px;
    left: -80px;
    --bb-orb-pulse-delay: 400ms;
    --bb-orb-transform: translate3d(calc(var(--shift) * -6vw + var(--px) * 22px), calc(var(--py) * 16px), 0);
  }

  /* The exit: the room lights come up as the stage slides off, so the cut to
     the next page reads as arriving somewhere rather than as a fade to black. */
  .leaving .ring { opacity: 0.18; }
  .leaving .orb { --bb-orb-opacity: 0.3; }
  .leaving .dawn { opacity: 1; transform: translate3d(0, 0, 0) scaleX(1.15); }

  :global(:root[data-theme="light"]) .dawn { opacity: calc(0.1 + var(--progress) * 0.4); }

  @keyframes slowspin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  @media (max-width: 760px) {
    .ring {
      width: 520px;
      height: 520px;
      right: -220px;
    }
  }

  /* Orbs freeze in orbs.css; the field hides itself in light-field.css. What
     is left to switch off here is this file's own motion. */
  @media (prefers-reduced-motion: reduce) {
    .dawn,
    .field,
    .ring,
    .orb { transition: none; }
    .ring svg { animation: none; }
  }
</style>
