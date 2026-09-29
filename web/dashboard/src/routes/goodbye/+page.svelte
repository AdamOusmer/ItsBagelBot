<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { AuroraBg, ButtonLink, Eyebrow, Heading, Lead, getI18n } from '@bagel/kit';

  const { t } = getI18n();

  const HOME = 'https://itsbagelbot.com';
  const DELAY_MS = 5000;

  let leaving = $state(false);

  onMount(() => {
    localStorage.removeItem('bb-onboarded');
    let exit: ReturnType<typeof setTimeout>;
    const t = setTimeout(() => {
      leaving = true;
      exit = setTimeout(() => (window.location.href = HOME), 700);
    }, DELAY_MS);
    return () => {
      clearTimeout(t);
      clearTimeout(exit);
    };
  });
</script>

<svelte:head>
  <title>{t('goodbye.title')}</title>
  <noscript><meta http-equiv="refresh" content="5;url=https://itsbagelbot.com" /></noscript>
</svelte:head>

<AuroraBg />

<main class="onboard" class:leaving>
  <div class="logo pop">
    <img src="/logo.png" alt="ItsBagelBot" />
    <span class="halo"></span>
  </div>

  <span class="eyebrow reveal"><Eyebrow tone="go">{t('goodbye.eyebrow')}</Eyebrow></span>

  <div class="hero reveal">
    <Heading level={1}>{t('goodbye.heroPre')}<span class="accent">{t('goodbye.heroAccent')}</span></Heading>
  </div>

  <div class="lede reveal">
    <Lead>{t('goodbye.lede')}</Lead>
  </div>

  <div class="cta reveal">
    <ButtonLink href={HOME} variant="green" solid>{t('goodbye.cta')}</ButtonLink>
  </div>

  <span class="bar reveal" aria-hidden="true"><i></i></span>
</main>

<style>
  .onboard {
    --bb-entrance: bb-rise-in;

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
    transition: opacity var(--bb-dur-slow) ease, filter var(--bb-dur-slow) ease;
  }
  .onboard.leaving { opacity: 0; filter: blur(6px); }

  .pop { opacity: 0; animation: pop calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo) both; }
  @keyframes pop { from { opacity: 0; transform: scale(0.6); filter: blur(12px); } to { opacity: 1; transform: none; filter: none; } }

  .logo { position: relative; width: 84px; height: 84px; display: grid; place-items: center; margin-bottom: 6px; }
  .logo img { width: 72px; height: 72px; border-radius: var(--bb-radius-sm); animation: float 6s ease-in-out infinite; }
  .logo .halo { position: absolute; inset: -22px; border-radius: 50%; background: radial-gradient(circle, rgba(var(--bb-green-glow-rgb), 0.35), transparent 68%); filter: blur(8px); animation: pulse 3.2s ease-in-out infinite; }

  .eyebrow { --i: 2.25; }

  .hero { --i: 3.75; max-width: 16ch; margin: 4px 0; }
  .accent { color: var(--bb-tan-light); }

  .lede { --i: 7.5; max-width: 52ch; margin: 4px 0 6px; }

  .cta { --i: 10.5; }

  .bar { --i: 12.5; margin-top: 8px; width: 180px; height: 3px; border-radius: var(--bb-radius-pill); background: rgba(var(--bb-white-pure-rgb), 0.06); border: 1px solid var(--bb-border); overflow: hidden; }
  .bar i { display: block; height: 100%; width: 100%; background: var(--bb-green); transform-origin: left; animation: drain 5s linear forwards; }

  @keyframes float { 0%, 100% { transform: translateY(0) rotate(-1deg); } 50% { transform: translateY(-10px) rotate(1deg); } }
  @keyframes pulse { 0%, 100% { opacity: 0.55; transform: scale(1); } 50% { opacity: 0.9; transform: scale(1.12); } }
  @keyframes drain { from { transform: scaleX(1); } to { transform: scaleX(0); } }

  @media (prefers-reduced-motion: reduce) {
    .pop, .logo img, .logo .halo, .bar i { animation: none; opacity: 1; }
    .onboard.leaving { opacity: 1; filter: none; }
  }
</style>