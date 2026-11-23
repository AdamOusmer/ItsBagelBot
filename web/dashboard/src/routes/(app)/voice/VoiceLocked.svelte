<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import { DEFAULT_VOICE } from '@bagel/kit/voice-plan';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import VoiceTuner from './VoiceTuner.svelte';

  const { t } = getI18n();
  let voice = $state(DEFAULT_VOICE);
</script>

<section class="stage bb-canvas-fill bb-no-orbs" aria-labelledby="voice-locked-title">
  <div class="field" aria-hidden="true"><LightField /></div>
  <div class="bb-orb bb-orb--halo bb-orb--green halo" aria-hidden="true"><i class="bb-orb__pulse"></i></div>
  <div class="inner">
    <header class="head">
      <span class="eyebrow">{t('billing.premium')}</span>
      <h1 id="voice-locked-title" class="title"><bb-fit><span data-fit="">{t('voice.lockedTitle')}</span></bb-fit></h1>
      <p class="lead bb-fit-box"><bb-fit mode="block">{t('voice.lockedBody')}</bb-fit></p>
    </header>
    <div class="tuner">
      <VoiceTuner {voice} onVoice={(next) => (voice = next)} orbSize="var(--orb)" autoplay />
    </div>
    <div class="actions">
      <form method="POST" action="/billing?/subscribe">
        <input type="hidden" name="plan" value="monthly" />
        <Button type="submit" variant="primary">{t('billing.premiumGateCta')}</Button>
      </form>
      <TextLink href="/billing" label={t('billing.premiumGateLink')} />
    </div>
  </div>
</section>

<style>
  .stage {
    --stage-top: calc(var(--bb-topbar-height) + 6px);
    position: relative;
    display: grid;
    flex: 1;
    place-items: center;
    min-inline-size: 0;
    margin-block-start: calc(-1 * var(--stage-top));
    block-size: 100dvh;
    overflow: hidden;
    padding: calc(var(--stage-top) + 24px) clamp(16px, 4vw, 48px) 24px;
    background-color: var(--bb-black);
  }
  .field { position: absolute; inset: 0; pointer-events: none; }
  .halo {
    --bb-orb-opacity: 0.1;
    inline-size: 760px;
    block-size: 760px;
    inset-block-start: 50%;
    inset-inline-start: 50%;
    translate: -50% -50%;
  }
  .inner {
    --orb: clamp(150px, 34dvh, 340px);
    --wheel-rows: 3;
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    justify-items: center;
    gap: clamp(16px, 3dvh, 32px);
    inline-size: min(640px, 100%);
    text-align: center;
  }
  .head { display: grid; grid-template-columns: minmax(0, 1fr); justify-items: center; gap: var(--bb-space-3); inline-size: 100%; }
  .eyebrow { font-size: 15px; font-weight: 700; color: var(--bb-tier-paid); }
  .title {
    inline-size: 100%;
    margin: 0;
    font-family: var(--bb-font-display);
    font-size: clamp(30px, 5dvh, 52px);
    font-weight: 500;
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.1;
  }
  .lead {
    inline-size: min(46ch, 100%);
    block-size: 3.2em;
    margin: 0;
    font-size: clamp(15px, 2dvh, 18px);
    line-height: 1.6;
    color: rgba(var(--bb-white-rgb), 0.8);
  }
  .tuner { display: grid; justify-items: center; gap: var(--bb-space-3); inline-size: min(100%, max(var(--orb), 260px)); }
  .actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: center; gap: 12px 20px; }
  @media (max-width: 760px) {
    .stage {
      --stage-top: calc(var(--bb-topbar-height) + 10px);
      padding-block-end: calc(var(--bb-dock-clearance) + 12px);
    }
    .inner { --orb: clamp(110px, 22dvh, 180px); }
    .lead { block-size: 4.8em; }
  }
</style>
