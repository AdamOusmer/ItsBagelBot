<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { parallax } from '@bagel/ui/svelte/actions';
  import AmbientSky from '@bagel/ui/svelte/AmbientSky.svelte';
  import Stepper from '@bagel/ui/svelte/Stepper.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';

  let { label, name, step, done, railSteps, maxStep, onSelect, expression, sequenceKey, backLabel, footer, later, aside, children }: {
    label: string; name: string; step: number; done: boolean; railSteps: { label: string }[];
    maxStep: number; onSelect: (target: number) => void; expression: string; sequenceKey: number;
    backLabel: string; footer: string; later: string; aside?: Snippet; children: Snippet;
  } = $props();
  const { t } = getI18n();
  let px = $state(0);
  let py = $state(0);
  const blobRight = $derived(step % 2 === 1);
  function follow(point: { px: number; py: number }) {
    px = point.px;
    py = point.py;
  }
</script>

<div class="setup-stage" role="region" aria-label={label} data-orbs="off" data-canvas="fill" use:parallax={{ onmove: follow }}>
  <AmbientSky position="contained" shift={blobRight ? 1 : -1} turn={step * 24} {px} {py} progress={done ? 1 : step / (railSteps.length - 1)} leaving={done} />
  <header class="top">
    <TextLink variant="quiet" icon="arrowLeft" href="/modules" label={backLabel} />
    <Stepper compact steps={railSteps} current={step} {maxStep} label={t('onboarding.stepOf', { n: step + 1, total: railSteps.length })} {onSelect} />
  </header>
  <div class="stage">
    <div class="pair">
      <div class="blob-col" class:right={blobRight}>
        <div class="scale" class:hero={step === 0 || done}>
          <div class="float"><div class="plate" aria-hidden="true"></div>
            <Bolota {name} size={240} active follow cycle={false}
              {expression}
              sequence={done ? 'burst' : 'entrance'} {sequenceKey} sequenceFor={1100} sequenceHold={240} />
          </div>
        </div>
        {@render aside?.()}
      </div>
      <div class="copy-col" class:left={blobRight}>
        {@render children()}
      </div>
    </div>
  </div>
  <footer><Text as="span" size="xs" mono tone="muted">{footer}</Text><TextLink variant="quiet" href="/modules" label={`${later} ↗`} /></footer>
</div>

<style>
  .setup-stage { --gap: clamp(24px, min(4vw, 6vh), 64px); --setup-gutter: clamp(24px, 4vw, 56px); --setup-title-size: clamp(2rem, min(3.8vw, 4.2vh), 3.5rem); --setup-kicker-gap: min(18px, 1.25vw, 1.2vh); --setup-title-gap: min(22px, 1.6vw, 1.6vh); --setup-controls-gap: min(26px, 2vw, 2vh); --setup-actions-gap: min(26px, 2vw, 2vh); position: relative; isolation: isolate; width: 100%; min-height: calc(100svh - 76px); display: grid; grid-template-rows: auto 1fr auto; overflow: clip; }
  .top, .stage, footer { position: relative; z-index: 1; }
  .top { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: min(24px, 2vw, 2vh) var(--setup-gutter); }
  .stage { display: grid; place-items: center; padding: min(20px, 1.5vw, 1.5vh) var(--setup-gutter) min(40px, 3vw, 3vh); }
  .pair { width: min(100%, 1080px); display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: var(--gap); min-height: min(460px, 40vw, 50vh); align-items: start; }
  .blob-col, .copy-col { transition: transform calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo); min-width: 0; }
  .blob-col { display: flex; flex-direction: column; align-items: center; justify-content: center; align-self: center; gap: 12px; }
  .blob-col.right { transform: translateX(calc(100% + var(--gap))); }
  .copy-col.left { transform: translateX(calc(-100% - var(--gap))); }
  .scale { --scale: .86; width: 240px; height: calc(240px * var(--scale)); transform: scale(var(--scale)); transform-origin: center top; transition: transform calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo); }
  .scale.hero { --scale: 1.1; }
  .float { position: relative; filter: drop-shadow(0 18px 34px rgba(var(--bb-shadow-rgb), .42)); animation: float 9s ease-in-out infinite; }
  .plate { position: absolute; inset: -22%; border-radius: 50%; background: radial-gradient(circle at 40% 35%, rgba(var(--bb-green-glow-rgb), .34), rgba(var(--bb-tan-rgb), .16) 46%, transparent 70%); filter: blur(22px); animation: breathe 6s ease-in-out infinite; z-index: -1; }
  .copy-col { display: grid; padding-top: min(48px, 3.4vw, 2.7vh); }
  footer { padding: min(20px, 1.5vw, 1.5vh) var(--setup-gutter) min(28px, 2vw, 2vh); display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 16px; }
  @keyframes float { 0%, 100% { transform: translateY(0) rotate(-2deg); } 50% { transform: translateY(-12px) rotate(2deg); } }
  @keyframes breathe { 0%, 100% { opacity: .7; transform: scale(.94); } 50% { opacity: 1; transform: scale(1.06); } }
  @media (max-width: 1050px) and (min-width: 761px) { .setup-stage { --setup-title-size: min(2.4rem, 3.8vw, 4.2vh); } .scale.hero { --scale: .88; } .scale { --scale: .72; } }
  @media (min-width: 761px) and (max-height: 860px) {
    .setup-stage { --setup-title-size: clamp(1.8rem, min(3.2vw, 4.2vh), 2.7rem); --setup-kicker-gap: 10px; --setup-title-gap: 12px; --setup-controls-gap: 16px; --setup-actions-gap: 16px; }
    .top { padding-block: 16px; }
    .stage { padding-top: 12px; padding-bottom: 20px; }
    .pair { min-height: 400px; }
    .copy-col { padding-top: 24px; }
    .scale { --scale: .72; }
    .scale.hero { --scale: .88; }
    footer { padding-top: 12px; padding-bottom: 16px; }
  }
  @media (min-width: 761px) and (max-height: 760px) {
    .setup-stage { --setup-title-size: clamp(1.65rem, min(2.8vw, 4.2vh), 2.3rem); --setup-kicker-gap: 8px; --setup-title-gap: 10px; --setup-controls-gap: 12px; --setup-actions-gap: 12px; }
    .top { padding-block: 12px; }
    .stage { padding-top: 8px; padding-bottom: 12px; }
    .pair { min-height: 360px; }
    .copy-col { padding-top: 8px; }
    .scale { --scale: .64; }
    .scale.hero { --scale: .78; }
    footer { padding-top: 8px; padding-bottom: 12px; }
  }
  @media (max-width: 760px) {
    .setup-stage { --setup-title-size: clamp(1.4rem, min(6.5vw, 3vh), 2rem); --setup-kicker-gap: 8px; --setup-title-gap: min(10px, 2vw, 1.2vh); --setup-controls-gap: 12px; --setup-actions-gap: 12px; min-height: calc(100svh - 68px); padding-bottom: 92px; }
    .top { padding-block: 12px; }
    .pair { grid-template-columns: minmax(0, 1fr); min-height: 0; gap: 4px; }
    .blob-col, .blob-col.right { transform: none; gap: 0; }
    .scale, .scale.hero { --scale: .2; }
    .copy-col, .copy-col.left { transform: none; padding-top: 0; width: min(100%, 520px); justify-self: center; }
    .stage { padding-top: 0; padding-bottom: 8px; }
    footer { padding-top: 8px; padding-bottom: 10px; gap: 8px; }
  }
  @media (prefers-reduced-motion: reduce) {
    .blob-col, .copy-col, .scale { transition: none; }
    .float, .plate { animation: none; }
  }
</style>
