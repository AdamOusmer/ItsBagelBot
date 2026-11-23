<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import type { Snippet } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { DEFAULT_VOICE } from '@bagel/kit/voice-plan';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import { PREMIUM_MONTHLY_CAD } from '#lib/premium-price';
  import VoiceTuner from './VoiceTuner.svelte';

  let { children }: { children: Snippet } = $props();

  const { t, locale } = getI18n();
  const price = new Intl.NumberFormat(locale, { style: 'currency', currency: 'CAD', maximumFractionDigits: 0 }).format(PREMIUM_MONTHLY_CAD);
  let voice = $state(DEFAULT_VOICE);
</script>

<section class="sky bb-canvas-fill bb-no-orbs" aria-labelledby="voice-locked-title">
  <div class="field" aria-hidden="true"><LightField /></div>
  <div class="bb-orb bb-orb--halo bb-orb--green glow" aria-hidden="true"><i class="bb-orb__pulse"></i></div>
  <div class="bb-orb bb-orb--halo bb-orb--tan warm" aria-hidden="true"><i class="bb-orb__pulse"></i></div>
  <div class="head">{@render children()}</div>
  <div class="stage">
    <VoiceTuner {voice} onVoice={(next) => (voice = next)} orbSize="var(--orb)" autoplay>
      {#snippet lead()}
        <h2 id="voice-locked-title" class="title">
          <span class="name"><bb-fit><span data-fit="">{t('voice.lockedTitle')}</span></bb-fit></span>
          <span class="tag"><bb-fit><span data-fit="">{t('voice.lockedTag')}</span></bb-fit></span>
        </h2>
      {/snippet}
      {#snippet tail()}
        <form class="buy" method="POST" action="/billing?/subscribe">
          <p class="price"><span class="amount">{price}</span><span class="per"><bb-fit><span data-fit="">{t('billing.perMonth')}</span></bb-fit></span></p>
          <input type="hidden" name="plan" value="monthly" />
          <Button type="submit" variant="primary" block>{t('billing.premiumGateCta')}</Button>
        </form>
      {/snippet}
    </VoiceTuner>
  </div>
</section>

<style>
  .sky {
    --sky-top: calc(var(--bb-topbar-height) + 6px);
    position: relative;
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    flex: 1;
    min-inline-size: 0;
    margin-block-start: calc(-1 * var(--sky-top));
    block-size: 100dvh;
    overflow: hidden;
    padding: calc(var(--sky-top) + var(--gutter, 28px) + 6px) var(--gutter, 28px) 24px;
    padding-inline: max(var(--gutter, 28px), calc((100% - var(--shell-canvas-max, 1160px)) / 2 + var(--gutter, 28px)));
    background-color: var(--bb-black);
  }
  .field { position: absolute; inset: 0; pointer-events: none; }
  .glow {
    --bb-orb-opacity: 0.16;
    inline-size: 760px;
    block-size: 760px;
    inset-block-start: 55%;
    inset-inline-start: 40%;
    translate: -50% -50%;
  }
  .warm {
    --bb-orb-opacity: 0.05;
    --bb-orb-pulse-delay: 400ms;
    inline-size: 420px;
    block-size: 420px;
    inset-block-end: -220px;
    inset-inline-end: -160px;
  }
  .head { position: relative; }
  .stage {
    --orb: clamp(200px, min(50dvh, 32vw), 420px);
    position: relative;
    display: grid;
    grid-template-columns: var(--orb) minmax(0, 320px);
    place-content: center;
    align-items: center;
    gap: clamp(32px, 6vw, 96px);
    min-block-size: 0;
  }
  .title { display: grid; gap: var(--bb-space-2); margin: 0; text-align: center; }
  .name {
    font-family: var(--bb-font-display);
    font-size: clamp(34px, 3.6vw, 52px);
    font-weight: 500;
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.1;
    background: linear-gradient(180deg, var(--bb-white) 40%, rgba(var(--bb-tan-light-rgb), 0.55));
    background-clip: text;
    -webkit-background-clip: text;
    color: transparent;
  }
  .tag { font-size: 15px; font-weight: 700; color: var(--bb-tier-paid); }
  .buy { display: grid; gap: var(--bb-space-3); }
  .price {
    display: grid;
    grid-template-columns: auto minmax(0, max-content);
    justify-content: center;
    align-items: baseline;
    gap: var(--bb-space-2);
    margin: 0;
    font-variant-numeric: tabular-nums;
  }
  .amount { font-family: var(--bb-font-display); font-size: 32px; font-weight: 500; color: var(--bb-tier-paid); }
  .per { min-inline-size: 0; font-size: 13px; color: var(--bb-muted); }
  @media (max-width: 900px) {
    .stage {
      --orb: clamp(140px, min(50vw, 27dvh), 300px);
      --wheel-rows: 3;
      grid-template-columns: minmax(0, 320px);
      justify-items: center;
      gap: var(--bb-space-5);
    }
    .glow { inset-inline-start: 50%; inset-block-start: 40%; }
  }
  @media (max-width: 760px) {
    .sky {
      --sky-top: calc(var(--bb-topbar-height) + 10px);
      padding-block-end: calc(var(--bb-dock-clearance) + 12px);
    }
  }
</style>
