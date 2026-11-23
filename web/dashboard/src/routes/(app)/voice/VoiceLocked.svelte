<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import { DEFAULT_VOICE } from '@bagel/kit/voice-plan';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { PREMIUM_MONTHLY_CAD } from '#lib/premium-price';
  import VoiceTuner from './VoiceTuner.svelte';

  const { t, locale } = getI18n();
  const price = new Intl.NumberFormat(locale, { style: 'currency', currency: 'CAD', maximumFractionDigits: 0 }).format(PREMIUM_MONTHLY_CAD);
  let voice = $state(DEFAULT_VOICE);
</script>

<div class="stage">
  <VoiceTuner {voice} onVoice={(next) => (voice = next)} orbSize="var(--orb)" autoplay>
    {#snippet lead()}
      <h2 class="title bb-fit-box"><bb-fit mode="block">{t('voice.lockedTitle')}</bb-fit></h2>
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

<style>
  .stage {
    --orb: clamp(200px, min(50dvh, 32vw), 420px);
    display: grid;
    grid-template-columns: var(--orb) minmax(0, 320px);
    place-content: center;
    align-items: center;
    gap: clamp(32px, 6vw, 96px);
    min-block-size: calc(100dvh - var(--bb-topbar-height) - 200px);
  }
  .title {
    block-size: 2.3em;
    margin: 0;
    font-family: var(--bb-font-display);
    font-size: clamp(24px, 2.6vw, 36px);
    font-weight: 500;
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.1;
    text-align: center;
  }
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
      min-block-size: 0;
      text-align: center;
    }
  }
</style>
