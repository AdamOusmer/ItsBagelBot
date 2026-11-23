<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import AudioOrb from '@bagel/ui/svelte/AudioOrb.svelte';
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';

  let {
    label,
    oncomplete
  }: {
    label: string;
    oncomplete: () => void;
  } = $props();

  type Phase = 'drawing' | 'done' | 'leaving';
  const CHARGE_MS = 880;
  const ENERGY_START = 0.15;
  const ENERGY_CHARGED = 0.6;

  let phase = $state<Phase>('drawing');
  let energy = $state(ENERGY_START);

  onMount(() => {
    const timers: ReturnType<typeof setTimeout>[] = [];
    const later = (fn: () => void, delay: number) => timers.push(setTimeout(fn, delay));
    let raf = 0;

    const charge = (startedAt: number) => (now: number) => {
      const progress = Math.min((now - startedAt) / CHARGE_MS, 1);
      energy = ENERGY_START + (ENERGY_CHARGED - ENERGY_START) * (1 - (1 - progress) ** 3);
      if (progress < 1) raf = requestAnimationFrame(charge(startedAt));
    };

    if (prefersReducedMotion()) {
      phase = 'done';
      energy = 1;
      later(oncomplete, 900);
    } else {
      raf = requestAnimationFrame((now) => charge(now)(now));
      later(() => { phase = 'done'; energy = 1; cancelAnimationFrame(raf); }, CHARGE_MS);
      later(() => { phase = 'leaving'; }, 2000);
      later(oncomplete, 2520);
    }

    return () => {
      cancelAnimationFrame(raf);
      timers.forEach(clearTimeout);
    };
  });
</script>

<div class="completion" class:done={phase === 'done'} class:leaving={phase === 'leaving'}>
  <div class="aura" aria-hidden="true"></div>
  <div class="moment" role="status" aria-live="polite" aria-atomic="true">
    <div class="stage" aria-hidden="true">
      <span class="ripple"></span>
      <div class="orb"><AudioOrb analyser={null} {energy} /></div>
      <svg class="ring" viewBox="0 0 100 100" fill="none">
        <circle class="track" cx="50" cy="50" r="48" />
        <circle class="progress" cx="50" cy="50" r="48" pathLength="100" />
        <path class="check" d="m36 51 10 10 19-22" pathLength="100" />
      </svg>
    </div>
    <span class="label">{phase === 'drawing' ? '' : label}</span>
  </div>
</div>

<style>
  .completion {
    --stage: min(260px, 60vw, 42svh);
    position: fixed;
    inset: 0;
    z-index: var(--bb-z-overlay);
    display: grid;
    place-items: center;
    overflow: hidden;
    isolation: isolate;
    transition: opacity var(--bb-dur-slow) var(--bb-ease-out-expo);
  }

  .completion.leaving { opacity: 0; }

  .aura {
    position: absolute;
    width: min(88vw, 700px);
    aspect-ratio: 1;
    border-radius: var(--bb-radius-pill);
    background: radial-gradient(circle, rgba(var(--bb-green-glow-rgb), .16), rgba(var(--bb-tan-rgb), .07) 42%, transparent 70%);
    transform: scale(.72);
    opacity: 0;
    animation: arrive-aura calc(var(--bb-dur-slow) * 2) var(--bb-ease-out-expo) both;
  }

  .moment {
    position: relative;
    display: grid;
    place-items: center;
    width: var(--stage);
    height: var(--stage);
  }

  .stage {
    position: absolute;
    inset: 0;
    transition: transform var(--bb-dur-slow) var(--bb-ease-out-expo);
  }

  .leaving .stage { transform: scale(1.12); }

  .orb {
    position: absolute;
    inset: 14px;
    border-radius: var(--bb-radius-pill);
    overflow: hidden;
    opacity: 0;
    transform: scale(.6);
    animation: orb-in calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo) forwards;
  }

  .ripple {
    position: absolute;
    inset: 0;
    border-radius: var(--bb-radius-pill);
    box-shadow: 0 0 0 1px var(--bb-tan-pale);
    opacity: 0;
  }
  .done .ripple { animation: ripple calc(var(--bb-dur-slow) * 2) var(--bb-ease-out-expo) both; }

  .ring {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
  }

  .track,
  .progress { transform: rotate(-90deg); transform-origin: 50px 50px; }

  .track,
  .progress,
  .check {
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .track { stroke: var(--bb-border-strong); stroke-width: .3; }

  .progress {
    stroke: var(--bb-tan-pale);
    stroke-width: .9;
    stroke-dasharray: 100;
    stroke-dashoffset: 100;
    animation: draw-ring calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo) forwards;
  }

  .check {
    stroke: var(--bb-white);
    stroke-width: 1.8;
    stroke-dasharray: 100;
    stroke-dashoffset: 100;
    opacity: 0;
  }

  .done .check,
  .leaving .check {
    opacity: 1;
    animation: draw-ring var(--bb-dur-slow) var(--bb-ease-out-expo) forwards;
  }

  .label {
    position: absolute;
    top: calc(100% + var(--bb-space-4));
    left: 50%;
    width: max-content;
    max-width: min(80vw, 320px);
    text-align: center;
    overflow-wrap: anywhere;
    font: 600 clamp(18px, 3vw, 25px)/1.15 var(--bb-font-display);
    color: var(--bb-white);
    opacity: 0;
    letter-spacing: .12em;
    filter: blur(4px);
    transform: translate(-50%, 25px);
    transition:
      opacity var(--bb-dur-base) ease,
      letter-spacing var(--bb-dur-slow) var(--bb-ease-out-expo),
      transform var(--bb-dur-slow) var(--bb-ease-out-expo),
      filter var(--bb-dur-base) ease;
  }
  .done .label,
  .leaving .label { opacity: 1; letter-spacing: 0; filter: none; transform: translate(-50%, 0); }

  @keyframes draw-ring { to { stroke-dashoffset: 0; } }
  @keyframes orb-in { to { opacity: 1; transform: scale(1); } }
  @keyframes arrive-aura { to { opacity: 1; transform: scale(1); } }
  @keyframes ripple {
    0% { opacity: .7; transform: scale(1); }
    100% { opacity: 0; transform: scale(1.6); }
  }

  @media (prefers-reduced-motion: reduce) {
    .completion,
    .stage,
    .label { transition: none; }
    .aura,
    .orb,
    .progress,
    .done .check,
    .leaving .check,
    .done .ripple { animation: none; }
    .ripple { display: none; }
    .aura { opacity: 1; transform: none; }
    .orb { opacity: 1; transform: none; }
    .progress,
    .check { stroke-dashoffset: 0; }
    .done .label { opacity: 1; letter-spacing: 0; filter: none; transform: translate(-50%, 0); }
  }
</style>
