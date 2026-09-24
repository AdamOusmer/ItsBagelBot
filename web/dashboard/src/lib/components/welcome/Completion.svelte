<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';

  let {
    label,
    oncomplete
  }: {
    label: string;
    oncomplete: () => void;
  } = $props();

  type Phase = 'drawing' | 'done' | 'leaving';
  let phase = $state<Phase>('drawing');

  onMount(() => {
    const timers: ReturnType<typeof setTimeout>[] = [];
    const later = (fn: () => void, delay: number) => timers.push(setTimeout(fn, delay));

    if (prefersReducedMotion()) {
      phase = 'done';
      later(oncomplete, 900);
    } else {
      later(() => { phase = 'done'; }, 880);
      later(() => { phase = 'leaving'; }, 2000);
      later(oncomplete, 2520);
    }

    return () => timers.forEach(clearTimeout);
  });
</script>

<div class="completion" class:done={phase === 'done'} class:leaving={phase === 'leaving'}>
  <div class="aura" aria-hidden="true"></div>
  <div class="moment" role="status" aria-live="polite" aria-atomic="true">
    <div class="seal" aria-hidden="true">
      <svg class="circle" viewBox="0 0 180 180" fill="none">
        <circle class="track" cx="90" cy="90" r="77" />
        <circle class="progress" cx="90" cy="90" r="77" pathLength="100" />
        <path class="check" d="m69 63 13 13 28-29" />
      </svg>
      <span class="inner-glow"></span>
      <span class="ripple"></span>
      {#each Array.from({ length: 8 }) as _, i (i)}
        <span class="spark" style="--a: {i * 45 + 22.5}deg; --d: {i % 2 ? 1 : 0.78};"></span>
      {/each}
    </div>
    <span class="label">{phase === 'drawing' ? '' : label}</span>
  </div>
</div>

<style>
  .completion {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: grid;
    place-items: center;
    overflow: hidden;
    isolation: isolate;
    transition: opacity 520ms var(--bb-ease-out-expo, cubic-bezier(.16, 1, .3, 1));
  }

  .completion.leaving { opacity: 0; }

  .aura {
    position: absolute;
    width: min(88vw, 700px);
    aspect-ratio: 1;
    border-radius: 50%;
    background: radial-gradient(circle, rgba(var(--bb-green-glow-rgb, 64, 145, 108), .16), rgba(var(--bb-tan-rgb, 201, 168, 124), .07) 42%, transparent 70%);
    transform: scale(.72);
    opacity: 0;
    animation: arrive-aura 1100ms var(--bb-ease-out-expo, cubic-bezier(.16, 1, .3, 1)) both;
  }

  .moment {
    position: relative;
    display: grid;
    place-items: center;
    width: 180px;
    height: 180px;
    transition: transform 520ms var(--bb-ease-out-expo, cubic-bezier(.16, 1, .3, 1));
  }

  .leaving .moment { transform: scale(1.06); }

  .ripple {
    position: absolute;
    inset: 13px;
    border-radius: 50%;
    border: 1px solid var(--bb-tan-pale, #dcc6a4);
    opacity: 0;
  }
  .done .ripple { animation: ripple 1100ms var(--bb-ease-out-expo, cubic-bezier(.16, 1, .3, 1)) both; }

  .spark {
    position: absolute;
    top: 50%;
    left: 50%;
    width: 5px;
    height: 5px;
    margin: -2.5px 0 0 -2.5px;
    border-radius: 50%;
    background: var(--bb-green-glow, #40916c);
    box-shadow: 0 0 10px rgba(var(--bb-green-glow-rgb, 64, 145, 108), .8);
    opacity: 0;
    transform: rotate(var(--a)) translateX(70px) scale(.4);
  }
  .spark:nth-of-type(odd) { background: var(--bb-tan-pale, #dcc6a4); }
  .done .spark { animation: spark 900ms var(--bb-ease-out-expo, cubic-bezier(.16, 1, .3, 1)) 80ms both; }

  .seal,
  .circle {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
  }

  .circle { transform: rotate(-90deg); }

  .track,
  .progress,
  .check {
    stroke-linecap: round;
    stroke-linejoin: round;
    vector-effect: non-scaling-stroke;
  }

  .track { stroke: var(--bb-border-strong, rgba(255,255,255,.14)); stroke-width: 1; }

  .progress {
    stroke: var(--bb-tan-pale, #dcc6a4);
    stroke-width: 3;
    stroke-dasharray: 100;
    stroke-dashoffset: 100;
    animation: draw-circle 880ms var(--bb-ease-out-expo, cubic-bezier(.16, 1, .3, 1)) forwards;
  }

  .check {
    stroke: var(--bb-green-glow, #40916c);
    stroke-width: 4;
    stroke-dasharray: 70;
    stroke-dashoffset: 70;
    opacity: 0;
    transform: rotate(90deg);
    transform-origin: 90px 90px;
  }

  .done .check,
  .leaving .check {
    opacity: 1;
    animation: draw-check 480ms var(--bb-ease-out-expo, cubic-bezier(.16, 1, .3, 1)) forwards;
  }

  .inner-glow {
    position: absolute;
    inset: 24px;
    border-radius: 50%;
    box-shadow: 0 0 55px rgba(var(--bb-green-glow-rgb, 64, 145, 108), .16);
    opacity: 0;
    transition: opacity 550ms ease;
  }
  .done .inner-glow,
  .leaving .inner-glow { opacity: 1; }

  .label {
    position: relative;
    display: block;
    max-width: 120px;
    text-align: center;
    overflow-wrap: anywhere;
    font: 600 clamp(18px, 3vw, 25px)/1.15 var(--bb-font-display, sans-serif);
    color: var(--bb-white, #fff);
    opacity: 0;
    letter-spacing: .12em;
    filter: blur(4px);
    transform: translateY(25px);
    transition:
      opacity 440ms ease,
      letter-spacing 700ms var(--bb-ease-out-expo, cubic-bezier(.16, 1, .3, 1)),
      filter 440ms ease;
  }
  .done .label,
  .leaving .label { opacity: 1; letter-spacing: 0; filter: none; }

  @keyframes draw-circle { to { stroke-dashoffset: 0; } }
  @keyframes draw-check { to { stroke-dashoffset: 0; } }
  @keyframes arrive-aura { to { opacity: 1; transform: scale(1); } }
  @keyframes ripple {
    0% { opacity: .7; transform: scale(1); }
    100% { opacity: 0; transform: scale(1.65); }
  }
  @keyframes spark {
    0% { opacity: 0; transform: rotate(var(--a)) translateX(70px) scale(.4); }
    30% { opacity: 1; }
    100% { opacity: 0; transform: rotate(var(--a)) translateX(calc(118px * var(--d))) scale(1); }
  }

  @media (prefers-reduced-motion: reduce) {
    .completion,
    .moment,
    .inner-glow,
    .label { transition: none; }
    .aura,
    .progress,
    .done .check,
    .leaving .check,
    .done .ripple,
    .done .spark { animation: none; }
    .ripple,
    .spark { display: none; }
    .aura { opacity: 1; transform: none; }
    .progress,
    .check { stroke-dashoffset: 0; }
    .done .label { opacity: 1; letter-spacing: 0; filter: none; }
  }
</style>
