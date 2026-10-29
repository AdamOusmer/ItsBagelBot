<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { playHeroTitle } from '#lib/login-hero';
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import '@bagel/ui/styles/orbs.css';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { getI18n } from '@bagel/kit';
  import { SITE } from '@bagel/kit/site-links';
  import PublicHead from '#lib/components/public/PublicHead.svelte';

  const { t } = getI18n();

  const PALETTES = [
    { head: '#62a593', eye: '#111113' },
    { head: '#eadbbd', eye: '#111113' },
    { head: '#f6efe1', eye: '#111113' }
  ];

  type Row = 'back' | 'mid' | 'front' | 'hero';
  type Seat = { name: string; x: number; row: Row; size: number; palette: number; wide?: boolean };

  const SEATS: Seat[] = [
    { name: 'ttv_marguerite', x: 3, row: 'back', size: 64, palette: 1, wide: true },
    { name: 'noodle_arms', x: 13, row: 'back', size: 72, palette: 2, wide: true },
    { name: 'glazed_val', x: 26, row: 'back', size: 60, palette: 0, wide: true },
    { name: 'byte_sized_ben', x: 36, row: 'back', size: 70, palette: 1 },
    { name: 'seedling_sky', x: 49, row: 'back', size: 58, palette: 2 },
    { name: 'the_other_sam', x: 62, row: 'back', size: 66, palette: 2 },
    { name: 'caramel_cass', x: 19, row: 'back', size: 56, palette: 0, wide: true },
    { name: 'pixel_pavlova', x: 80, row: 'back', size: 60, palette: 1, wide: true },
    { name: 'oven_mitt', x: 72, row: 'back', size: 74, palette: 0, wide: true },
    { name: 'frosty_fern', x: 86, row: 'back', size: 62, palette: 1, wide: true },
    { name: 'ash_runs_it', x: 97, row: 'back', size: 68, palette: 2, wide: true },

    { name: 'lumen_tv', x: 6, row: 'mid', size: 104, palette: 2, wide: true },
    { name: 'dusty_crumbs', x: 21, row: 'mid', size: 118, palette: 0 },
    { name: 'sesame_sam', x: 36, row: 'mid', size: 96, palette: 1 },
    { name: 'tobi_plays', x: 50, row: 'mid', size: 88, palette: 2, wide: true },
    { name: 'crumb_lord', x: 64, row: 'mid', size: 110, palette: 1 },
    { name: 'kiki_bakes', x: 79, row: 'mid', size: 122, palette: 2 },
    { name: 'moonpie_mo', x: 94, row: 'mid', size: 100, palette: 0, wide: true },

    { name: 'nightfall_jo', x: 11, row: 'front', size: 196, palette: 1 },
    { name: 'ren_exe', x: 30, row: 'front', size: 172, palette: 2 },
    { name: 'oven_fresh', x: 50, row: 'hero', size: 290, palette: 0 },
    { name: 'mirafox', x: 70, row: 'front', size: 180, palette: 1 },
    { name: 'kassidy_mod', x: 89, row: 'front', size: 204, palette: 2 }
  ];

  const titleWords = $derived(`${t('login.title1')} ${t('login.title2')}`.split(' '));

  const NOTICES: Record<string, string> = {
    signedout: t('login.noticeSignedout'),
    revoked: t('login.noticeRevoked'),
    banned: t('login.noticeBanned'),
    link: t('login.noticeLink'),
    retry: t('login.noticeRetry'),
    imp: t('login.noticeImpersonation'),
    declined: t('login.noticeDeclined'),
    state: t('login.noticeState'),
    oauth: t('login.noticeOauth'),
    bot: t('login.noticeBot'),
    scope: t('login.noticeScope')
  };
  const notice = $derived(NOTICES[page.url.searchParams.get('e') ?? ''] ?? null);

  const next = $derived(page.url.searchParams.get('next'));
  const loginHref = $derived(next ? `/auth/login?next=${encodeURIComponent(next)}` : '/auth/login');

  onMount(playHeroTitle);
</script>

<PublicHead title={t('login.metaTitle')} description={t('login.metaDescription')} url="https://dashboard.itsbagelbot.com/login" />

<header class="hero">
  <div class="starfield" aria-hidden="true">
    <LightField class="bb-light-field--bleed" />
  </div>

  <div class="bg" aria-hidden="true">
    <div class="bb-orb bb-orb--halo bb-orb--green orb orb-1"></div>
    <div class="bb-orb bb-orb--halo bb-orb--tan orb orb-2"></div>
  </div>

  <div class="material">
    {#if notice}
      <div class="notice">
        <AlertBanner tone="danger">{notice}</AlertBanner>
      </div>
    {/if}

    <div class="eyebrow">
      <span class="eyebrow__beta"><span class="eyebrow__beta-label"><span data-fit>{t('login.badge')}</span></span></span>
      <Brand title="ItsBagelBot" logoSrc="/logo.png" wordmark size="lg" class="eyebrow__brand" />
    </div>

    <h1 data-hero-title aria-label={titleWords.join(' ')}><span data-fit="block" aria-hidden="true">{#each titleWords as word, i (i)}{#if i > 0}{' '}{/if}<span class="word" data-hero-word>{word}</span>{/each}</span></h1>

    <Text tone="muted-soft" size="md" class="lede"><span data-fit="block">{t('login.lede')}</span></Text>

    <div class="actions">
      <ButtonLink class="cta" href={loginHref} variant="twitch" data-sveltekit-reload><span data-fit>{t('login.cta')}</span></ButtonLink>
      <ButtonLink class="cta" variant="secondary" href={SITE.web}><span data-fit>{t('login.back')}</span></ButtonLink>
    </div>

    <p class="consent">{@html t('login.consent')}</p>
  </div>

  <div class="crowd" aria-hidden="true">
    {#each SEATS as seat, i (seat.name)}
      <div
        class="seat seat--{seat.row}"
        class:seat--wide={seat.wide}
        style="--x: {seat.x}%; --s: {seat.size}px; --d: {(i * 37) % 11}"
      >
        <div class="bob">
          <Bolota name={seat.name} size={seat.size} palette={PALETTES[seat.palette]} motionState="idle" active gate class="seat__bolota" />
        </div>
      </div>
    {/each}
  </div>
</header>

<style>
  .hero {
    --hero-pad-x: 40px;
    --crowd-h: 360px;
    --crowd-scale: 1;
    --nav-offset: calc(var(--bb-nav-height) + env(safe-area-inset-top, 0px));

    position: relative;
    isolation: isolate;
    height: 100vh;
    height: 100svh;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: flex-start;
    text-align: center;
    padding: calc(var(--nav-offset) + clamp(40px, 8vh, 88px)) var(--hero-pad-x) 0;
    overflow: hidden;
  }

  .material {
    display: flex;
    flex-direction: column;
    align-items: center;
    width: min(100%, 1160px);
    position: relative;
    z-index: 2;
  }

  .starfield {
    position: absolute;
    inset: 0;
    z-index: -1;
    pointer-events: none;
  }

  .bg {
    position: absolute;
    inset: 0;
    pointer-events: none;
    overflow: hidden;
  }

  .orb {
    --bb-orb-opacity: 0.16;
    --bb-orb-core-opacity: 0;
  }

  .orb-1 {
    width: 520px;
    height: 520px;
    top: -140px;
    right: -80px;
  }

  .orb-2 {
    width: 420px;
    height: 420px;
    bottom: -120px;
    left: -100px;
    --bb-orb-pulse-delay: 400ms;
  }

  .notice {
    max-width: 60ch;
    margin-bottom: 20px;
    text-align: left;
  }

  .eyebrow {
    --hero-brand-size: min(calc((100vw - var(--hero-pad-x) * 2) / 9), 5.2rem);

    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 18px;
    margin-bottom: 28px;
    opacity: 0;
    animation: fadeUp 800ms 150ms var(--bb-ease-out-expo) forwards;
  }

  .eyebrow :global(.eyebrow__brand) {
    --brand-name-size: var(--hero-brand-size);
    flex: none;
    height: calc(var(--hero-brand-size) * 1.12);
  }

  .eyebrow :global(.eyebrow__brand .bb-brand__name) {
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.12;
    background: linear-gradient(180deg, var(--bb-white) 40%, rgba(var(--bb-tan-light-rgb), 0.55));
    background-clip: text;
    -webkit-background-clip: text;
    color: transparent;
    filter: drop-shadow(0 0 36px rgba(var(--bb-tan-rgb), 0.18));
  }

  .eyebrow__beta {
    display: inline-flex;
    align-items: center;
    flex: none;
    width: 84px;
    height: 24px;
    font-family: var(--bb-font-body);
    font-size: var(--bb-text-md);
    font-weight: 700;
    letter-spacing: 0.02em;
    color: var(--bb-green-glow);
    text-shadow: 0 0 18px rgba(var(--bb-green-glow-rgb), 0.45);
  }

  .eyebrow__beta-label {
    flex: 1 1 0;
    min-width: 0;
  }

  h1 {
    --h1-lines: 1;

    display: flex;
    flex-direction: column;
    justify-content: safe center;
    font-family: var(--bb-font-body);
    font-size: clamp(1.35rem, 2.2vw, 2.1rem);
    font-weight: 600;
    line-height: 1.2;
    letter-spacing: -0.01em;
    color: var(--bb-tan-pale);
    text-wrap: balance;
    width: min(100%, 900px);
    height: calc(var(--h1-lines) * 1lh);
    margin-top: 0;
    margin-bottom: 20px;
    overflow: hidden;
  }

  .word {
    display: inline-block;
    transform-origin: 20% 90%;
    transition: none;
  }

  .material :global(.lede) {
    --lede-lines: 2;

    width: min(100%, 560px);
    height: calc(var(--lede-lines) * 1lh);
    display: flex;
    flex-direction: column;
    justify-content: safe center;
    line-height: 1.6;
    margin-bottom: 36px;
    opacity: 0;
    animation: fadeUp 0.9s 0.55s var(--bb-ease-out-expo) forwards;
  }

  .actions {
    display: flex;
    justify-content: center;
    gap: 12px;
    width: 100%;
    opacity: 0;
    animation: fadeUp 0.9s 0.75s var(--bb-ease-out-expo) forwards;
  }

  .actions :global(.cta) {
    --btn-w: 216px;
    --btn-justify: center;
    --btn-min-h: 48px;
    --btn-pad: 13px 16px;
  }

  .consent {
    margin: 16px 0 0;
    font-family: var(--bb-font-body);
    font-size: 0.78rem;
    line-height: 1.5;
    color: var(--bb-muted);
    opacity: 0;
    animation: fadeUp 0.9s 0.85s var(--bb-ease-out-expo) forwards;
  }

  .consent :global(a) {
    color: var(--bb-tan-light);
    text-decoration: none;
  }

  .consent :global(a:hover) {
    color: var(--bb-white);
  }

  .crowd {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    height: var(--crowd-h);
    z-index: 1;
    pointer-events: none;
  }

  .seat {
    position: absolute;
    left: var(--x);
    bottom: var(--row-bottom);
    width: calc(var(--s) * var(--crowd-scale));
    height: calc(var(--s) * var(--crowd-scale));
    transform: translateX(-50%);
    opacity: var(--row-opacity, 1);
  }

  .seat--back {
    --row-bottom: 60%;
    --row-opacity: 0.5;
  }

  .seat--mid {
    --row-bottom: 30%;
    --row-opacity: 0.8;
  }

  .seat--front {
    --row-bottom: -6%;
  }

  .seat--hero {
    --row-bottom: -8%;
    z-index: 2;
  }

  .bob {
    width: 100%;
    height: 100%;
    animation:
      riseIn 1.1s calc(0.5s + var(--d) * 0.09s) var(--bb-ease-out-back) both,
      bob 6.5s calc(var(--d) * -0.7s) ease-in-out infinite alternate;
  }

  .bob :global(.seat__bolota) {
    width: 100%;
    height: 100%;
  }

  @media (scripting: enabled) and (prefers-reduced-motion: no-preference) {
    .word {
      opacity: 0;
      transform: translateY(112%) rotate(3deg);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .eyebrow,
    .material :global(.lede),
    .actions,
    .consent {
      opacity: 1;
      animation: none;
    }

    .bob {
      animation: none;
    }
  }

  @keyframes fadeUp {
    from {
      opacity: 0;
      transform: translateY(24px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  @keyframes riseIn {
    from {
      opacity: 0;
      transform: translateY(70%) scale(0.86);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }

  @keyframes bob {
    from { translate: 0 0; }
    to { translate: 0 -7px; }
  }

  @media (max-height: 820px) and (min-width: 901px) {
    .hero {
      --crowd-h: 280px;
      padding-top: calc(var(--nav-offset) + 32px);
    }

    .eyebrow {
      --hero-brand-size: min(calc((100vw - var(--hero-pad-x) * 2) / 9), 4rem);
      gap: 14px;
      margin-bottom: 22px;
    }

    h1 {
      margin-bottom: 16px;
    }

    .material :global(.lede) {
      margin-bottom: 28px;
    }
  }

  @media (max-width: 900px) {
    .hero {
      --hero-pad-x: 20px;
      --crowd-h: 36svh;
      --crowd-scale: 0.74;
      height: auto;
      min-height: 100svh;
      padding: calc(var(--nav-offset) + 40px) var(--hero-pad-x) 0;
    }

    .crowd {
      position: relative;
      flex: 1 0 var(--crowd-h);
      width: calc(100% + var(--hero-pad-x) * 2);
      margin-top: 20px;
    }

    .eyebrow {
      gap: 14px;
      margin-bottom: 20px;
    }

    h1 {
      --h1-lines: 2;
      font-size: clamp(1.2rem, 4.6vw, 1.8rem);
      margin-bottom: 16px;
    }

    .material :global(.lede) {
      --lede-lines: 3;
      font-size: var(--bb-text-md);
      line-height: 1.55;
      margin-bottom: 28px;
    }

    .seat--wide {
      display: none;
    }

    .seat--back {
      --row-bottom: 78%;
    }

    .seat--mid {
      --row-bottom: 45%;
    }
  }

  @media (max-width: 480px) {
    .hero {
      --hero-pad-x: 16px;
      --crowd-h: 34svh;
      --crowd-scale: 0.64;
      padding-top: calc(var(--nav-offset) + 32px);
    }

    .eyebrow {
      gap: 12px;
      margin-bottom: 18px;
    }

    .material :global(.lede) {
      font-size: var(--bb-text-sm);
      margin-bottom: 24px;
    }

    .actions {
      flex-direction: column;
      align-items: center;
      gap: 10px;
    }

    .actions :global(.cta) {
      --btn-w: 240px;
    }
  }
</style>
