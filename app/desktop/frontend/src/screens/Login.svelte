<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AmbientSky from '@bagel/ui/svelte/AmbientSky.svelte';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { authState, startPairing, retryAuthorization } from '../lib/auth.svelte';
  import { mountEngine } from '@luzir/bolota/engine';
  import '@luzir/bolota/motion.css';
  import { Browser } from '@wailsio/runtime';
  import { onMount } from 'svelte';
  import login from '../../../../../locales/en/console/login.json';

  type Row = 'back' | 'front' | 'hero';
  type Seat = { name: string; x: number; row: Row; size: number; head: string };

  const SEATS: Seat[] = [
    { name: 'noodle_arms', x: 7, row: 'back', size: 96, head: '--bb-tan-pale' },
    { name: 'byte_sized_ben', x: 24, row: 'back', size: 84, head: '--bb-tan-light' },
    { name: 'seedling_sky', x: 41, row: 'back', size: 78, head: '--bb-tan-pale' },
    { name: 'the_other_sam', x: 60, row: 'back', size: 88, head: '--bb-tan-pale' },
    { name: 'oven_mitt', x: 77, row: 'back', size: 92, head: '--bb-green-glow' },
    { name: 'ash_runs_it', x: 94, row: 'back', size: 86, head: '--bb-tan-pale' },
    { name: 'nightfall_jo', x: 13, row: 'front', size: 176, head: '--bb-tan-light' },
    { name: 'ren_exe', x: 31, row: 'front', size: 150, head: '--bb-tan-pale' },
    { name: 'oven_fresh', x: 50, row: 'hero', size: 248, head: '--bb-green-glow' },
    { name: 'mirafox', x: 69, row: 'front', size: 158, head: '--bb-tan-light' },
    { name: 'kassidy_mod', x: 87, row: 'front', size: 182, head: '--bb-tan-pale' },
  ];

  const words = `${login.title1} ${login.title2}`.split(' ');

  let svgs: SVGSVGElement[] = $state([]);

  // A webview ignores target=_blank, so web links have to leave through the OS browser.
  function openExternally(event: MouseEvent): void {
    const link = (event.target as Element).closest<HTMLAnchorElement>('a[href^="http"]');
    if (!link) return;
    event.preventDefault();
    Browser.OpenURL(link.href).catch(() => window.open(link.href, '_blank', 'noopener'));
  }

  onMount(() => {
    const styles = getComputedStyle(document.documentElement);
    const eye = styles.getPropertyValue('--bb-black').trim();
    const engines = SEATS.map((seat, i) =>
      mountEngine(svgs[i], seat.name, {
        size: seat.size,
        palette: { head: styles.getPropertyValue(seat.head).trim(), eye },
        background: false,
      }),
    );

    document.addEventListener('click', openExternally);
    return () => {
      document.removeEventListener('click', openExternally);
      engines.forEach((engine) => engine.destroy());
    };
  });
</script>

<main class="auth">
  <AmbientSky position="contained" />
  <div class="auth__drag" aria-hidden="true"></div>

  <div class="auth__material">
    <Brand title="ItsBagelBot" logoSrc="/logo.png" wordmark size="lg" class="auth__brand" />
    <h1 class="auth__title">
      {#each words as word, i (i)}<span class="auth__word" style="--w: {i}">{word}</span>{' '}{/each}
    </h1>
    <Button variant="twitch" disabled={!authState.configured} busy={authState.pending || authState.working} onclick={() => { void (authState.paired ? retryAuthorization() : startPairing()); }}>{authState.paired ? 'Reconnect' : login.cta}</Button>
    {#if authState.pending}<p role="status">Approve this device in your browser.</p>{/if}
    {#if authState.error}<p role="status">{authState.error}</p>{/if}
    <p class="auth__consent">{@html login.consent}</p>
  </div>

  <div class="crowd" aria-hidden="true">
    {#each SEATS as seat, i (seat.name)}
      <div class="seat seat--{seat.row}" style="--x: {seat.x}%; --s: {seat.size}; --d: {i}">
        <svg bind:this={svgs[i]} viewBox="0 0 100 100"></svg>
      </div>
    {/each}
  </div>
</main>

<style>
  .auth {
    --crowd-unit: clamp(0.55px, min(100vw / 1280, 100svh / 820), 1px);
    position: relative;
    isolation: isolate;
    display: flex;
    flex-direction: column;
    align-items: center;
    height: 100svh;
    padding: 52px 24px 0;
    overflow: hidden;
    text-align: center;
  }

  .auth :global(.bb-ambient-sky) {
    z-index: -1;
  }

  .auth__drag {
    --wails-draggable: drag;
    position: absolute;
    top: 8px;
    left: 80px;
    right: 24px;
    z-index: 1;
    height: 44px;
  }

  .auth__material {
    display: flex;
    flex: none;
    flex-direction: column;
    align-items: center;
    gap: 16px;
    width: min(100%, 760px);
    margin-block: auto;
  }

  .auth__material :global(.auth__brand) {
    --brand-name-size: clamp(2.4rem, 8.5vw, 5.2rem);
    height: auto;
  }

  :global(.auth__brand .bb-brand__name) {
    background: linear-gradient(180deg, var(--bb-white) 40%, rgba(var(--bb-tan-light-rgb), 0.55));
    background-clip: text;
    -webkit-background-clip: text;
    color: transparent;
    line-height: 1.12;
    letter-spacing: var(--bb-tracking-display);
  }

  .auth__title {
    color: var(--bb-tan-pale);
    font-family: var(--bb-font-body);
    font-size: clamp(1.35rem, 2.6vw, 2.1rem);
    font-weight: 600;
    line-height: 1.2;
    letter-spacing: -0.01em;
    text-wrap: balance;
  }

  .auth__word {
    display: inline-block;
    animation: word-in 0.9s cubic-bezier(0.22, 1, 0.36, 1) both;
    animation-delay: calc(0.2s + var(--w) * 0.1s);
  }

  @keyframes word-in {
    from {
      transform: translateY(60%) rotate(3deg);
      opacity: 0;
    }
  }

  .auth__consent {
    color: var(--bb-muted);
    font-size: var(--bb-text-xs);
  }

  .auth__consent :global(a) {
    color: var(--bb-tan-light);
    text-decoration: underline;
    text-underline-offset: 2px;
  }

  .crowd {
    position: relative;
    flex: none;
    align-self: stretch;
    height: calc(216 * var(--crowd-unit));
    margin-inline: -24px;
    pointer-events: none;
  }

  .seat {
    --side: calc(var(--s) * var(--crowd-unit));
    position: absolute;
    left: var(--x);
    bottom: calc(-14 * var(--crowd-unit));
    width: var(--side);
    height: var(--side);
    margin-left: calc(var(--side) / -2);
    transform-origin: 50% 100%;
    animation: seat-enter 900ms calc(300ms + var(--d) * 45ms) cubic-bezier(0.34, 1.56, 0.64, 1) both;
  }

  .seat svg {
    width: 100%;
    height: 100%;
    overflow: visible;
    animation: seat-bob 5s calc(var(--d) * -0.7s) ease-in-out infinite;
  }

  .seat--back {
    bottom: calc(96 * var(--crowd-unit));
    opacity: 0.42;
  }

  .seat--hero {
    z-index: 2;
    bottom: calc(-32 * var(--crowd-unit));
  }

  @keyframes seat-enter {
    from {
      transform: translateY(70%);
      opacity: 0;
    }
  }

  @keyframes seat-bob {
    50% {
      transform: translateY(-6px);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .auth__word,
    .seat,
    .seat svg {
      animation: none;
    }
  }
</style>
