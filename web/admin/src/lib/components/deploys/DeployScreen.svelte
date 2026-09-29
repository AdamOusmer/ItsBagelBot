<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import { onNavigate } from '$app/navigation';
  import { hasFinePointer, prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
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

<div class="screen" data-orbs="off">
  <header class="top">
    <Brand title="ItsBagelBot" sub={t('admin.title')} href="/" logoSrc="/logo.png" logoAlt="" size="md" />
    <div class="ident">
      <Eyebrow>{eyebrow}</Eyebrow>
      <Heading level={5} as="span">{name} {#if em}<em class="em">{em}</em>{/if}</Heading>
    </div>
    <div class="status">
      {@render status?.()}
      <ButtonLink href={closeHref} variant="secondary" size="sm">{closeLabel}</ButtonLink>
    </div>
  </header>

  <main class="body">
    <aside class="side">
      <Card glass flush>
        <div class="side-body">{@render side()}</div>
      </Card>
    </aside>
    <section class="theatre">{@render children()}</section>
  </main>

  <footer class="foot">
    <span class="count">{count}</span>
    <div class="meter">
      <ProgressBar value={clamped} tone="success" size="sm" gradient label={progressLabel} />
      <span class="bead-track" style:--p={clamped} aria-hidden="true"><span class="bead"></span></span>
    </div>
    <span class="pct">{percent}</span>
  </footer>
</div>

<ToastHost />

<style>
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
    gap: var(--bb-space-3) var(--bb-space-5);
    flex-wrap: wrap;
    padding: var(--bb-space-5) var(--gutter) 0;
    animation: settle calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo) backwards;
  }
  .ident {
    display: grid;
    gap: var(--bb-space-1);
    min-width: 0;
  }
  .em {
    font-style: normal;
    color: var(--bb-tan-pale);
  }
  .status {
    display: flex;
    align-items: center;
    gap: var(--bb-space-4);
    flex-wrap: wrap;
    margin-left: auto;
  }

  .body {
    display: grid;
    grid-template-columns: 300px minmax(0, 1fr);
    gap: var(--gap);
    width: min(100%, 1480px);
    margin-inline: auto;
    padding: var(--bb-space-5) var(--gutter);
  }

  .side {
    align-self: start;
    animation: settle calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo) 80ms backwards;
  }
  .side-body {
    padding: var(--bb-space-2);
  }
  .theatre {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-5);
    min-width: 0;
  }

  .foot {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--bb-space-4);
    padding: var(--bb-space-3) var(--gutter) var(--bb-space-4);
    animation: settle calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo) 120ms backwards;
  }
  .count,
  .pct {
    font-family: var(--bb-font-mono);
    font-size: var(--bb-text-xs);
    letter-spacing: var(--bb-tracking-eyebrow);
    text-transform: uppercase;
    color: rgba(var(--bb-white-pure-rgb), 0.7);
    font-variant-numeric: tabular-nums;
  }
  .pct {
    min-width: 4ch;
    text-align: right;
    color: var(--bb-tan-pale);
  }
  .meter {
    --progress-from: var(--bb-tan);
    --progress-duration: calc(var(--bb-dur-slow) * 1.5);
    position: relative;
  }
  .bead-track {
    position: absolute;
    inset: 0;
    pointer-events: none;
    transform: translateX(calc(var(--p) * 100%));
    transition: transform var(--progress-duration) var(--bb-ease-out-expo);
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
    animation: breathe calc(var(--bb-dur-slow) * 5) ease-in-out infinite;
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
    animation-duration: calc(var(--bb-dur-slow) * 1.2);
    animation-timing-function: var(--bb-ease-out-expo);
  }

  @media (min-width: 961px) and (min-height: 640px) {
    .screen {
      height: 100svh;
    }
    .body {
      min-height: 0;
    }
    .side {
      display: grid;
      grid-template-rows: minmax(0, 1fr);
      min-height: 0;
      max-height: 100%;
    }
    .side-body {
      max-height: 100%;
      overflow-y: auto;
      scrollbar-width: thin;
    }
    .theatre {
      min-height: 0;
      overflow-y: auto;
      padding-right: var(--bb-space-2);
      scrollbar-width: thin;
    }
  }

  @media (max-width: 960px) {
    .body {
      grid-template-columns: minmax(0, 1fr);
      gap: var(--bb-space-5);
    }
    .status {
      margin-left: 0;
    }
  }

  @media (prefers-reduced-motion: reduce) {
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
