<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import { onNavigate } from '$app/navigation';
  import { hasFinePointer, prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import Sky from '@bagel/ui/svelte/Sky.svelte';
  import ToastHost from '@bagel/ui/svelte/ToastHost.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  let {
    eyebrow,
    name,
    em = '',
    closeHref,
    closeLabel,
    progress,
    progressLabel,
    count,
    turn,
    leaving = false,
    status,
    side,
    children
  }: {
    eyebrow: string;
    name: string;
    em?: string;
    closeHref: string;
    closeLabel: string;
    progress: number;
    progressLabel: string;
    count: string;
    turn: number;
    leaving?: boolean;
    status?: Snippet;
    side: Snippet;
    children: Snippet;
  } = $props();

  const { t, locale } = getI18n();

  const clamped = $derived(Math.min(Math.max(progress, 0), 1));
  const percent = $derived(new Intl.NumberFormat(locale, { style: 'percent' }).format(clamped));

  let px = $state(0);
  let py = $state(0);
  let pointerFrame = 0;
  function onPointerMove(e: PointerEvent) {
    if (!hasFinePointer() || pointerFrame) return;
    pointerFrame = requestAnimationFrame(() => {
      pointerFrame = 0;
      px = (e.clientX / window.innerWidth - 0.5) * 2;
      py = (e.clientY / window.innerHeight - 0.5) * 2;
    });
  }

  onNavigate((navigation) => {
    if (!document.startViewTransition || prefersReducedMotion()) return;
    return new Promise((resolve) => {
      const transition = document.startViewTransition(async () => {
        resolve();
        await navigation.complete;
      });
      transition.ready.catch(() => {});
    });
  });
</script>

<svelte:window onpointermove={onPointerMove} />

<Sky shift={turn % 2 ? 1 : -1} turn={turn * 24} {px} {py} progress={clamped} {leaving} />

<div class="screen" data-deploy-screen>
  <header class="top">
    <Brand title="ItsBagelBot" sub={t('admin.title')} href="/" logoSrc="/logo.png" logoAlt="" size="md" />
    <div class="ident">
      <span class="eyebrow">{eyebrow}</span>
      <span class="name">{name} {#if em}<em>{em}</em>{/if}</span>
    </div>
    <div class="status">
      {@render status?.()}
      <a class="close" href={closeHref}>{closeLabel}</a>
    </div>
  </header>

  <main class="body">
    <aside class="side">{@render side()}</aside>
    <section class="theatre">{@render children()}</section>
  </main>

  <footer class="foot">
    <span class="count">{count}</span>
    <div
      class="bar"
      role="progressbar"
      aria-label={progressLabel}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(clamped * 100)}
      style="--p: {clamped};"
    >
      <span class="fill"></span>
      <span class="bead-track"><span class="bead"></span></span>
    </div>
    <span class="pct">{percent}</span>
  </footer>
</div>

<ToastHost />

<style>
  :global(body:has([data-deploy-screen]) .bb-bg-orb) {
    display: none;
  }

  .screen {
    --gutter: clamp(16px, 3.5vw, 44px);
    --gap: clamp(20px, 3.5vw, 56px);
    position: relative;
    z-index: 1;
    min-height: 100svh;
    display: grid;
    grid-template-rows: auto minmax(0, 1fr) auto;
    overflow-x: clip;
  }

  .top {
    display: flex;
    align-items: center;
    gap: 12px 28px;
    flex-wrap: wrap;
    padding: 20px var(--gutter) 0;
    animation: settle 900ms var(--bb-ease-out-expo) backwards;
  }
  .ident {
    display: grid;
    gap: 2px;
    min-width: 0;
  }
  .eyebrow {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-tan);
  }
  .name {
    font: 700 17px/1.2 var(--bb-font-display);
    color: var(--bb-white);
  }
  .name em {
    font-style: normal;
    color: var(--bb-tan-pale);
  }
  .status {
    display: flex;
    align-items: center;
    gap: 16px;
    flex-wrap: wrap;
    margin-left: auto;
  }
  .close {
    padding: 8px 14px;
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-sm);
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 13px;
    color: var(--bb-white);
    text-decoration: none;
    transition:
      border-color var(--bb-dur-base) var(--bb-ease-out-expo),
      transform var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .close:hover {
    border-color: var(--bb-tan);
    transform: translateX(3px);
  }
  .close:focus-visible {
    outline: 2px solid var(--bb-tan);
    outline-offset: 2px;
  }

  .body {
    display: grid;
    grid-template-columns: 300px minmax(0, 1fr);
    gap: var(--gap);
    width: min(100%, 1480px);
    margin-inline: auto;
    padding: 24px var(--gutter);
  }

  .side {
    align-self: start;
    padding: 10px;
    background: linear-gradient(180deg, rgba(255, 255, 255, 0.045), rgba(0, 0, 0, 0.3));
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-lg);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.07),
      0 24px 60px rgba(0, 0, 0, 0.32);
    backdrop-filter: blur(12px);
    animation: settle 900ms var(--bb-ease-out-expo) 80ms backwards;
  }
  .theatre {
    display: flex;
    flex-direction: column;
    gap: 20px;
    min-width: 0;
  }

  .foot {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: 16px;
    padding: 12px var(--gutter) 18px;
    animation: settle 900ms var(--bb-ease-out-expo) 120ms backwards;
  }
  .count,
  .pct {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: rgba(255, 255, 255, 0.7);
    font-variant-numeric: tabular-nums;
  }
  .pct {
    min-width: 4ch;
    text-align: right;
    color: var(--bb-tan-pale);
  }
  .bar {
    position: relative;
    height: 4px;
    border-radius: var(--bb-radius-pill);
    background: rgba(255, 255, 255, 0.08);
  }
  .fill {
    position: absolute;
    inset: 0;
    border-radius: inherit;
    transform-origin: left;
    transform: scaleX(var(--p));
    background: linear-gradient(90deg, var(--bb-tan), var(--bb-green-glow));
    transition: transform 900ms var(--bb-ease-out-expo);
  }
  .bead-track {
    position: absolute;
    inset: 0;
    transform: translateX(calc(var(--p) * 100%));
    transition: transform 900ms var(--bb-ease-out-expo);
  }
  .bead {
    position: absolute;
    top: -3px;
    left: -5px;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--bb-green-glow);
    box-shadow:
      0 0 0 3px rgba(var(--bb-green-glow-rgb), 0.18),
      0 0 16px rgba(var(--bb-green-glow-rgb), 0.75);
    animation: breathe 3s ease-in-out infinite;
  }

  @keyframes settle {
    from {
      opacity: 0;
      transform: translateX(-16px);
    }
    to {
      opacity: 1;
      transform: none;
    }
  }
  @keyframes breathe {
    0%,
    100% {
      opacity: 0.75;
      transform: scale(1);
    }
    50% {
      opacity: 1;
      transform: scale(1.1);
    }
  }
  :global(::view-transition-old(root)),
  :global(::view-transition-new(root)) {
    animation-duration: 720ms;
    animation-timing-function: cubic-bezier(0.16, 1, 0.3, 1);
  }


  @media (min-width: 961px) and (min-height: 640px) {
    .screen {
      height: 100svh;
    }
    .body {
      min-height: 0;
    }
    .side {
      max-height: 100%;
      overflow-y: auto;
      scrollbar-width: thin;
    }
    .theatre {
      min-height: 0;
      overflow-y: auto;
      padding-right: 8px;
      scrollbar-width: thin;
    }
  }

  @media (max-width: 960px) {
    .body {
      grid-template-columns: minmax(0, 1fr);
      gap: 20px;
    }
    .side {
      padding: 6px;
    }
    .status {
      margin-left: 0;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .close,
    .fill,
    .bead-track {
      transition: none;
    }
    .top,
    .side,
    .foot,
    .bead {
      animation: none;
    }
  }
</style>
