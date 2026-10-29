<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import '@bagel/kit/styles/ui-console.css';
  import { onMount } from 'svelte';
  import ErrorScene from '@bagel/ui/svelte/ErrorScene.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { getI18n } from '@bagel/kit';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';

  const { t } = getI18n();

  const HOME = 'https://itsbagelbot.com';
  const DELAY_MS = 10000;
  const EXIT_MS = 700;

  let leaving = $state(false);
  let paused = $state(false);
  let sceneEl = $state<HTMLElement | null>(null);

  onMount(() => {
    localStorage.removeItem('bb-onboarded');
    const holds = { hover: false, focus: false, hidden: document.hidden };
    let remaining = DELAY_MS;
    let startedAt = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let exit: ReturnType<typeof setTimeout> | undefined;

    const leave = () => {
      timer = undefined;
      leaving = true;
      exit = setTimeout(() => (window.location.href = HOME), EXIT_MS);
    };
    const sync = () => {
      paused = holds.hover || holds.focus || holds.hidden;
      if (paused && timer) {
        clearTimeout(timer);
        timer = undefined;
        remaining -= performance.now() - startedAt;
      } else if (!paused && !timer && !leaving) {
        startedAt = performance.now();
        timer = setTimeout(leave, Math.max(remaining, 0));
      }
    };
    const hold = (key: keyof typeof holds, on: boolean) => () => {
      holds[key] = on;
      sync();
    };
    const onVisibility = () => {
      holds.hidden = document.hidden;
      sync();
    };
    const onFocusOut = (e: FocusEvent) => {
      holds.focus = !!sceneEl?.contains(e.relatedTarget as Node | null);
      sync();
    };
    const bindings: [EventTarget | null, string, EventListener][] = [
      [sceneEl, 'pointerenter', hold('hover', true)],
      [sceneEl, 'pointerleave', hold('hover', false)],
      [sceneEl, 'focusin', hold('focus', true)],
      [sceneEl, 'focusout', onFocusOut as EventListener],
      [document, 'visibilitychange', onVisibility]
    ];
    for (const [target, type, fn] of bindings) target?.addEventListener(type, fn);
    sync();
    return () => {
      for (const [target, type, fn] of bindings) target?.removeEventListener(type, fn);
      clearTimeout(timer);
      clearTimeout(exit);
    };
  });
</script>

<svelte:head>
  <title>{t('goodbye.title')}</title>
  <noscript><meta http-equiv="refresh" content="10;url=https://itsbagelbot.com" /></noscript>
</svelte:head>

<div class="goodbye" class:leaving bind:this={sceneEl}>
  <ErrorScene
    eyebrow={t('goodbye.eyebrow')}
    title={t('goodbye.heroPre') + t('goodbye.heroAccent')}
    description={t('goodbye.lede')}
    labelledBy="goodbye-title"
    primary={{ label: t('goodbye.cta'), href: HOME }}
  >
    {#snippet mark()}
      <Bolota name="oven_fresh" size={132} palette={{ head: '#62a593', eye: '#111113' }} motionState="idle" expression="sad" active />
    {/snippet}
    <span class="bar" aria-hidden="true"><i class:paused></i></span>
    <VisuallyHidden as="p" role="status">{paused ? t('goodbye.paused') : t('goodbye.redirecting')}</VisuallyHidden>
  </ErrorScene>
</div>

<style>
  .goodbye {
    transition: opacity var(--bb-dur-slow) ease, filter var(--bb-dur-slow) ease;
  }
  .goodbye.leaving { opacity: 0; filter: blur(6px); }

  .bar {
    margin-top: 32px;
    width: 180px;
    height: 3px;
    border-radius: var(--bb-radius-pill);
    background: rgba(var(--bb-tan-rgb), 0.1);
    overflow: hidden;
    opacity: 0;
    animation: rise 780ms 610ms var(--bb-ease-out-expo) forwards;
  }
  .bar i { display: block; height: 100%; width: 100%; background: var(--bb-tan); transform-origin: left; animation: drain 10s linear forwards; }
  .bar i.paused { animation-play-state: paused; }

  @keyframes rise { from { opacity: 0; transform: translateY(18px); } to { opacity: 1; transform: none; } }
  @keyframes drain { from { transform: scaleX(1); } to { transform: scaleX(0); } }

  @media (prefers-reduced-motion: reduce) {
    .bar { animation: none; opacity: 1; }
    .bar i { animation: none; }
    .goodbye.leaving { opacity: 1; filter: none; }
  }
</style>
