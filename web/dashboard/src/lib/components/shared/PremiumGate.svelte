<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { Bolota, getI18n } from '@bagel/kit';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';

  const { t } = getI18n();

  let {
    label = t('billing.premium'),
    title = t('billing.premiumGateTitle'),
    body = t('billing.premiumGateBody'),
    cta = t('billing.premiumGateCta'),
    link = t('billing.premiumGateLink')
  }:{ label?: string; title?: string; body?: string; cta?: string; link?: string } = $props();

  const TAN = { head: '#eadbbd', eye: '#111113' };

  const titleWords = $derived(title.split(' '));
</script>

<section class="gate" data-canvas="fill" data-orbs="off" aria-labelledby="premium-gate-title">
  <div class="field" aria-hidden="true"><LightField /></div>
  <div class="bb-orb bb-orb--halo bb-orb--green orb orb--green" aria-hidden="true"><i class="bb-orb__pulse"></i></div>
  <div class="bb-orb bb-orb--halo bb-orb--tan orb orb--tan" aria-hidden="true"><i class="bb-orb__pulse"></i></div>
  <div class="inner">
    <div class="layout">
      <div class="copy">
        <span class="glow">{label}</span>
        <h1 id="premium-gate-title" class="title" tabindex="-1" aria-label={title}>
          {#each titleWords as word, i (`${i}:${word}`)}
            {#if i > 0}{' '}{/if}<span class="word" aria-hidden="true" style="--w: {i}">{word}</span>
          {/each}
        </h1>
        <p class="lead rise" style="--i: 1">{body}</p>
        <div class="actions rise" style="--i: 2">
          <ButtonLink variant="primary" href="/billing?subscribe=1">{cta}</ButtonLink>
          <TextLink href="/billing" label={link} />
        </div>
      </div>
      <div class="mascot" aria-hidden="true">
        <Bolota name="oven_fresh" size={340} palette={TAN} active follow />
      </div>
    </div>
  </div>
</section>

<style>
  .gate {
    --gate-top: calc(var(--bb-topbar-height) + 6px);
    position: relative;
    display: flex;
    flex: 1;
    align-items: center;
    justify-content: center;
    min-width: 0;
    margin-top: calc(-1 * var(--gate-top));
    height: 100dvh;
    overflow: hidden;
    padding: calc(var(--gate-top) + 24px) clamp(16px, 4vw, 48px) 24px;
    background-color: var(--bb-black);
  }
  .field {
    position: absolute;
    inset: 0;
    pointer-events: none;
  }
  .orb--green { --bb-orb-opacity: 0.12; width: 520px; height: 520px; top: -140px; right: -80px; }
  .orb--tan { --bb-orb-opacity: 0.05; width: 420px; height: 420px; bottom: -240px; left: -200px; --bb-orb-pulse-delay: 400ms; }
  .inner {
    position: relative;
    container-type: inline-size;
    width: min(1120px, 100%);
    min-width: 0;
  }
  .layout {
    --mascot: clamp(150px, 28cqw, 320px);
    display: grid;
    grid-template-columns: minmax(0, 1fr) var(--mascot);
    align-items: center;
    column-gap: clamp(24px, 6cqw, 80px);
  }
  .copy {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    min-width: 0;
  }
  .mascot {
    position: relative;
    width: var(--mascot);
    height: var(--mascot);
    animation: mascot-in 1.1s 500ms var(--bb-ease-out-back) both;
  }
  .mascot :global(svg.bolota) {
    position: absolute;
    inset: -20%;
    display: block;
    width: 140%;
    height: 140%;
    overflow: visible;
  }
  @container (max-width: 560px) {
    .layout {
      --mascot: 150px;
      grid-template-columns: minmax(0, 1fr);
      row-gap: 40px;
    }
    .mascot {
      order: -1;
      justify-self: center;
    }
  }
  .glow {
    font-size: clamp(15px, 2.1vh, 18px);
    font-weight: 700;
    letter-spacing: 0.02em;
    color: var(--bb-tier-paid);
    text-shadow: 0 0 18px rgba(var(--bb-tan-rgb), 0.45);
    animation: fade 800ms 150ms var(--bb-ease-out-expo) both;
  }
  .title {
    margin: clamp(8px, 1.6vh, 14px) 0 clamp(16px, 3.4vh, 32px);
    overflow: hidden;
    padding-bottom: 0.08em;
    font-family: var(--bb-font-display);
    font-size: clamp(38px, min(7.2cqw, 10vh), 84px);
    font-weight: 500;
    letter-spacing: var(--bb-tracking-display);
    line-height: 1.06;
    text-wrap: balance;
    outline: none;
  }
  .word {
    display: inline-block;
    transform-origin: 20% 90%;
    background: linear-gradient(180deg, var(--bb-white) 40%, rgba(var(--bb-tan-light-rgb), 0.55));
    background-clip: text;
    -webkit-background-clip: text;
    color: transparent;
    animation: word-in 900ms calc(300ms + var(--w, 0) * 90ms) cubic-bezier(0.22, 1, 0.36, 1) both;
  }
  .lead {
    margin: 0 0 clamp(20px, 4vh, 36px);
    max-width: 46ch;
    font-size: clamp(16px, 2.3vh, 20px);
    line-height: 1.6;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px 20px;
  }
  .rise {
    animation: fade-up 900ms calc(450ms + var(--i, 0) * 120ms) var(--bb-ease-out-expo) both;
  }
  @keyframes fade {
    from { opacity: 0; }
    to { opacity: 1; }
  }
  @keyframes fade-up {
    from { opacity: 0; transform: translateY(24px); }
    to { opacity: 1; transform: none; }
  }
  @keyframes word-in {
    from { opacity: 0; transform: translateY(112%) rotate(3deg); }
    to { opacity: 1; transform: none; }
  }
  @keyframes mascot-in {
    from { opacity: 0; transform: translateY(30%) scale(0.86); }
    to { opacity: 1; transform: none; }
  }
  @media (max-width: 760px) {
    .gate {
      --gate-top: calc(var(--bb-topbar-height) + 10px);
      padding-bottom: calc(var(--bb-dock-clearance, 0px) + 12px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .glow,
    .word,
    .rise,
    .mascot {
      animation: none;
    }
  }
</style>
