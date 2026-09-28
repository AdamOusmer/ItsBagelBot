<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { AuroraBg, getI18n } from '@bagel/kit';

  const { t } = getI18n();

  const HOME = 'https://itsbagelbot.com';
  const DELAY_MS = 10000;
  const EXIT_MS = 700;

  let leaving = $state(false);
  let paused = $state(false);
  let mainEl = $state<HTMLElement | null>(null);

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
      holds.focus = !!mainEl?.contains(e.relatedTarget as Node | null);
      sync();
    };
    const bindings: [EventTarget | null, string, EventListener][] = [
      [mainEl, 'pointerenter', hold('hover', true)],
      [mainEl, 'pointerleave', hold('hover', false)],
      [mainEl, 'focusin', hold('focus', true)],
      [mainEl, 'focusout', onFocusOut as EventListener],
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

<AuroraBg />

<main class="onboard" class:leaving bind:this={mainEl}>
  <div class="logo pop" style="--d:0s">
    <img src="/logo.png" alt="ItsBagelBot" />
    <span class="halo"></span>
  </div>

  <div class="eyebrow reveal" style="--d:.18s">{t('goodbye.eyebrow')}</div>

  <h1 class="hero reveal" style="--d:.3s">{t('goodbye.heroPre')}<span class="accent">{t('goodbye.heroAccent')}</span></h1>

  <p class="lede reveal" style="--d:.6s">
    {t('goodbye.lede')}
  </p>

  <a class="cta reveal" style="--d:.85s" href={HOME}>{t('goodbye.cta')}</a>

  <span class="bar reveal" style="--d:1s" aria-hidden="true"><i class:paused></i></span>
  <p class="sr" role="status">{paused ? t('goodbye.paused') : t('goodbye.redirecting')}</p>
</main>

<style>
  .onboard {
    position: relative;
    z-index: 1;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    gap: 16px;
    padding: 24px;
    transition: opacity 0.7s ease, filter 0.7s ease;
  }
  .onboard.leaving { opacity: 0; filter: blur(6px); }

  .reveal { opacity: 0; animation: reveal 0.8s cubic-bezier(0.16, 1, 0.3, 1) both; animation-delay: var(--d, 0s); }
  .pop { opacity: 0; animation: pop 0.9s cubic-bezier(0.16, 1, 0.3, 1) both; animation-delay: var(--d, 0s); }
  @keyframes reveal { from { opacity: 0; transform: translateY(22px); } to { opacity: 1; transform: none; } }
  @keyframes pop { from { opacity: 0; transform: scale(0.6); filter: blur(12px); } to { opacity: 1; transform: none; filter: none; } }

  .logo { position: relative; width: 84px; height: 84px; display: grid; place-items: center; margin-bottom: 6px; }
  .logo img { width: 72px; height: 72px; border-radius: var(--bb-radius-sm); animation: float 6s ease-in-out infinite; }
  .logo .halo { position: absolute; inset: -22px; border-radius: 50%; background: radial-gradient(circle, rgba(82, 183, 136, 0.35), transparent 68%); filter: blur(8px); animation: pulse 3.2s ease-in-out infinite; }

  .eyebrow { font-family: var(--bb-font-mono); font-size: 12px; letter-spacing: 0.22em; text-transform: uppercase; color: var(--bb-green-glow); }
  .hero { font-family: var(--bb-font-display); font-weight: 700; font-size: clamp(34px, 6vw, 68px); line-height: 1; letter-spacing: -0.03em; color: var(--bb-white); margin: 4px 0; max-width: 16ch; }
  .hero .accent { color: var(--bb-tan-light); }

  .lede { font-family: var(--bb-font-body); font-size: clamp(15px, 1.6vw, 18px); color: var(--bb-muted); max-width: 52ch; line-height: 1.6; margin: 4px 0 6px; }

  .cta { display: inline-flex; align-items: center; gap: 10px; font-family: var(--bb-font-mono); font-size: 12px; letter-spacing: 0.12em; text-transform: uppercase; color: var(--bb-white); background: var(--bb-green); border: 1px solid var(--bb-green-light); padding: 15px 26px; border-radius: var(--bb-radius-pill); text-decoration: none; transition: background 0.3s, transform 0.3s; }
  .cta:hover { background: var(--bb-green-light); transform: translateY(-2px); box-shadow: 0 0 36px rgba(82, 183, 136, 0.4); }

  .bar { margin-top: 8px; width: 180px; height: 3px; border-radius: var(--bb-radius-pill); background: rgba(255, 255, 255, 0.06); border: 1px solid var(--bb-border); overflow: hidden; }
  .bar i { display: block; height: 100%; width: 100%; background: var(--bb-green); transform-origin: left; animation: drain 10s linear forwards; }
  .bar i.paused { animation-play-state: paused; }
  .sr { position: absolute; width: 1px; height: 1px; margin: -1px; padding: 0; overflow: hidden; clip-path: inset(50%); white-space: nowrap; border: 0; }

  @keyframes float { 0%, 100% { transform: translateY(0) rotate(-1deg); } 50% { transform: translateY(-10px) rotate(1deg); } }
  @keyframes pulse { 0%, 100% { opacity: 0.55; transform: scale(1); } 50% { opacity: 0.9; transform: scale(1.12); } }
  @keyframes drain { from { transform: scaleX(1); } to { transform: scaleX(0); } }

  @media (prefers-reduced-motion: reduce) {
    .reveal, .pop, .logo img, .logo .halo, .bar i { animation: none; opacity: 1; }
    .onboard.leaving { opacity: 1; filter: none; }
  }
</style>
