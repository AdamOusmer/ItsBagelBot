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
  <div class="head">{@render children()}</div>
  <div class="stage">
    <div class="bb-orb bb-orb--halo bb-orb--green aura" aria-hidden="true"><i class="bb-orb__pulse"></i></div>
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
  .head { position: relative; }
  .stage {
    --orb: clamp(200px, min(46dvh, 30vw), 380px);
    --side: 280px;
    --gap: clamp(40px, 5vw, 72px);
    position: relative;
    display: grid;
    grid-template-columns: var(--orb) var(--side);
    place-content: center;
    align-items: center;
    gap: var(--gap);
    min-block-size: 0;
  }
  .aura {
    --bb-orb-opacity: 0.16;
    position: absolute;
    inline-size: calc(var(--orb) * 2);
    block-size: calc(var(--orb) * 2);
    inset-block-start: 50%;
    inset-inline-start: calc(50% - (var(--orb) + var(--gap) + var(--side)) / 2 + var(--orb) / 2);
    translate: -50% -50%;
  }
  .title { display: grid; justify-items: center; gap: var(--bb-space-1); margin: 0; text-align: center; }
  .name {
    inline-size: 100%;
    font-family: var(--bb-font-display);
    font-size: 40px;
    font-weight: 500;
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.15;
    background: linear-gradient(180deg, var(--bb-white) 40%, rgba(var(--bb-tan-light-rgb), 0.55));
    background-clip: text;
    -webkit-background-clip: text;
    color: transparent;
  }
  .tag { inline-size: 100%; font-size: 14px; font-weight: 600; color: var(--bb-tier-paid); }
  .buy { display: grid; gap: var(--bb-space-4); }
  .price { display: grid; justify-items: center; gap: var(--bb-space-1); margin: 0; font-variant-numeric: tabular-nums; }
  .amount { font-family: var(--bb-font-display); font-size: 28px; font-weight: 500; line-height: 1; color: var(--bb-white); }
  .per { inline-size: 100%; font-size: 12px; color: var(--bb-muted); text-align: center; }
  @media (max-width: 900px) {
    .stage {
      --orb: clamp(140px, min(50vw, 27dvh), 300px);
      --wheel-rows: 3;
      grid-template-columns: minmax(0, var(--side));
      gap: var(--bb-space-5);
    }
    .aura { inset-inline-start: 50%; inset-block-start: calc(var(--orb) / 2); }
  }
  @media (max-width: 760px) {
    .sky {
      --sky-top: calc(var(--bb-topbar-height) + 10px);
      padding-block-end: calc(var(--bb-dock-clearance) + 12px);
    }
  }
</style>
