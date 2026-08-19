<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // First-visit onboarding: a roaming guide shown once when a new user reaches
  // the dashboard (they're already connected by then, so it starts at the step
  // people actually miss - modding the bot). Same five steps, same copy, same
  // guards as the old dialog stepper, but the blob walks the screen instead of
  // sitting still in a card. Dismissal is remembered in localStorage;
  // `?welcome=1` re-opens it for a refresher (both handled by the caller).
  import { Icon, Bolota, Toggle, getI18n } from '@bagel/shared';
  import LangSwitch from './LangSwitch.svelte';
  import CursorSwitch from './CursorSwitch.svelte';

  type Step = {
    title: string;
    body: string;
    lang?: boolean;
    mod?: boolean;
    consent?: boolean;
    cta?: { href: string; label: string };
  };

  // Anchor points as percentages of the viewport, one per step, in walking
  // order: centre, upper-right, lower-left, centre-right, back to centre. Kept
  // well inside the edges (max 78 / min 22) so the bubble's own width never
  // needs more room than the flip logic below already accounts for.
  const ANCHORS: { x: number; y: number }[] = [
    { x: 50, y: 50 },
    { x: 76, y: 26 },
    { x: 24, y: 74 },
    { x: 70, y: 46 },
    { x: 50, y: 50 }
  ];

  let { open = false, name, onDone }: { open: boolean; name: string; onDone: () => void } = $props();

  const { t } = getI18n();

  let consentAccepted = $state(false);

  const MOD_COMMAND = '/mod ItsBagelBot';
  let copied = $state(false);
  async function copyMod() {
    let ok = false;
    try {
      await navigator.clipboard.writeText(MOD_COMMAND);
      ok = true;
    } catch {
      // Clipboard API blocked (permissions/insecure context): legacy fallback.
      const ta = document.createElement('textarea');
      ta.value = MOD_COMMAND;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      try {
        ok = document.execCommand('copy');
      } catch {
        ok = false;
      }
      ta.remove();
    }
    if (ok) {
      copied = true;
      setTimeout(() => (copied = false), 2000);
    }
  }

  const steps: Step[] = [
    {
      title: t('onboarding.consentTitle'),
      body: t('onboarding.consentBody'),
      consent: true
    },
    {
      title: t('onboarding.langTitle'),
      body: t('onboarding.langBody'),
      lang: true
    },
    {
      title: t('onboarding.step1Title'),
      body: t('onboarding.step1Body'),
      mod: true
    },
    {
      title: t('onboarding.step2Title'),
      body: t('onboarding.step2Body'),
      cta: { href: '/commands', label: t('onboarding.step2Cta') }
    },
    {
      title: t('onboarding.step3Title'),
      body: t('onboarding.step3Body'),
      cta: { href: '/modules', label: t('onboarding.step3Cta') }
    }
  ];

  let step = $state(0);
  const last = $derived(step === steps.length - 1);
  const nextDisabled = $derived(steps[step].consent === true && !consentAccepted);
  const anchor = $derived(ANCHORS[step]);

  // The pair is centred on its anchor, so a percentage alone puts half of it
  // past the edge on a narrow window. Measure the pair and the viewport, then
  // clamp the centre into a corridor that is guaranteed to fit.
  const MARGIN = 20;
  let vw = $state(0);
  let vh = $state(0);
  let pairW = $state(0);
  let pairH = $state(0);
  const clampAxis = (pct: number, span: number, size: number) => {
    const half = size / 2 + MARGIN;
    const want = (pct / 100) * span;
    return span < size + MARGIN * 2 ? span / 2 : Math.min(Math.max(want, half), span - half);
  };
  const px = $derived(clampAxis(anchor.x, vw, pairW));
  const py = $derived(clampAxis(anchor.y, vh, pairH));

  // Bubble sits on the side of the blob that has room to breathe. Past the
  // 55% line the right edge is getting close, so the bubble goes left of the
  // blob (and vice versa); the flex layout below just reverses direction.
  const bubbleSide = $derived(anchor.x > 55 ? 'left' : 'right');

  // Hover reactions: the guide blob holds its resting face (no expression
  // rotation here, unlike the avatars) and only reacts to the pointer. Pleased
  // on the action that moves the flow forward, merely interested in anything
  // else it can click, back to resting on pointer-out.
  let hoverExpression = $state<string | null>(null);
  function reactionFor(target: EventTarget | null): string | null {
    const el = target instanceof Element ? target.closest('button, a') : null;
    if (!el || el.hasAttribute('disabled')) return null;
    return el.classList.contains('primary') ? 'excited' : 'curious';
  }

  // Every step change clears the hover reaction. A click moves the bubble (and
  // often re-renders the control under the pointer), so pointerout never fires
  // and the blob would otherwise hold the reaction face for good.
  function goNext() {
    hoverExpression = null;
    step += 1;
  }
  function goBack() {
    hoverExpression = null;
    step -= 1;
  }
  function goTo(i: number) {
    hoverExpression = null;
    step = i;
  }

  // Bubble is the actual dialog surface; focus it on open and on every step
  // change so screen readers land on the new copy instead of wherever the
  // page happened to be focused.
  let bubbleEl = $state<HTMLDivElement | null>(null);
  $effect(() => {
    step; // re-run on step change
    if (open) bubbleEl?.focus();
  });

  // Simple scroll lock while the guide is up, mirroring what Modal does via
  // the overlay stack but without pulling in stacking/inert semantics this
  // single, always-topmost surface doesn't need.
  $effect(() => {
    if (!open || typeof document === 'undefined') return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = prev;
    };
  });

  function onKeydown(e: KeyboardEvent) {
    if (open && e.key === 'Escape') {
      e.preventDefault();
      onDone();
    }
  }
</script>

<svelte:window onkeydown={onKeydown} bind:innerWidth={vw} bind:innerHeight={vh} />

{#if open}
  {@const s = steps[step]}
  <div class="guide-layer">
    <div class="scrim"></div>
    <div
      class="guide-wrap"
      style="--ax: {px}px; --ay: {py}px;"
    >
      <div
        class="pair {bubbleSide === 'left' ? 'side-left' : 'side-right'}"
        bind:clientWidth={pairW}
        bind:clientHeight={pairH}
      >
        <div class="blob-slot">
          <Bolota
            {name}
            size={120}
            active={open}
            follow
            cycle={false}
            sequence="entrance"
            sequenceKey={step}
            expression={hoverExpression}
            title={t('onboarding.title')}
          />
        </div>
        <div
          class="bubble"
          role="dialog"
          aria-modal="true"
          aria-labelledby="onb-title"
          tabindex="-1"
          bind:this={bubbleEl}
          onpointerover={(e) => (hoverExpression = reactionFor(e.target))}
          onpointerout={() => (hoverExpression = null)}
        >
          <span class="tail" aria-hidden="true"></span>
          <p class="kicker">{t('onboarding.title')}</p>
          {#if step === 0}
            <p class="intro">{t('onboarding.intro')}</p>
          {/if}

          {#key step}
            <div class="step">
              <h4 id="onb-title">{s.title}</h4>
              <p class="step-body">{s.body}</p>

              {#if s.consent}
                <div class="consent-check">
                  <Toggle bind:on={consentAccepted} />
                  <span>{@html t('onboarding.consentLabel')}</span>
                </div>
              {/if}

              {#if s.lang}
                <div class="lang-row"><LangSwitch /></div>
                <div class="pref-row">
                  <div>
                    <span class="pref-name">{t('settings.customCursor')}</span>
                    <p class="pref-hint" id="onb-cursor-hint">{t('settings.customCursorHint')}</p>
                  </div>
                  <CursorSwitch describedby="onb-cursor-hint" />
                </div>
              {/if}

              {#if s.mod}
                <button type="button" class="mod-cmd" onclick={copyMod} title={t('common.copy')}>
                  <code>{MOD_COMMAND}</code>
                  <span class="copy-hint">
                    <Icon name={copied ? 'check' : 'link'} size={12} />
                    {copied ? t('common.copied') : t('common.copy')}
                  </span>
                </button>
              {/if}

              {#if s.cta}
                <a class="btn ghost step-cta" href={s.cta.href} onclick={onDone}>
                  {s.cta.label}
                </a>
              {/if}
            </div>
          {/key}

          <div class="foot">
            <div class="dots" aria-label={t('onboarding.stepOf', { n: step + 1, total: steps.length })}>
              {#each steps as _, i (i)}
                <button
                  type="button"
                  class="dot {i === step ? 'on' : ''}"
                  aria-label={t('onboarding.goToStep', { n: i + 1 })}
                  onclick={() => goTo(i)}
                ></button>
              {/each}
            </div>
            <div class="nav">
              {#if step > 0}
                <button type="button" class="btn ghost" onclick={goBack}>{t('onboarding.back')}</button>
              {:else if !s.consent}
                <button type="button" class="btn ghost" onclick={onDone}>{t('onboarding.skip')}</button>
              {/if}
              {#if last}
                <button type="button" class="btn primary" onclick={onDone} disabled={nextDisabled}>{t('onboarding.done')}</button>
              {:else}
                <button type="button" class="btn primary" onclick={goNext} disabled={nextDisabled}>{t('onboarding.next')}</button>
              {/if}
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
{/if}

<style>
  .guide-layer {
    position: fixed;
    inset: 0;
    z-index: 400;
  }

  .scrim {
    position: absolute;
    inset: 0;
    background: rgba(10, 9, 8, 0.55);
    backdrop-filter: blur(2px);
  }

  /* Positions the blob+bubble pair at the current anchor; the trailing
     translate(-50%, -50%) centres the whole pair (not just the blob) on that
     point. Reduced-motion below drops the transition. */
  .guide-wrap {
    position: absolute;
    left: 0;
    top: 0;
    /* `--ax`/`--ay` arrive already clamped in px (see clampAxis), so the pair
       is centred on a point that is guaranteed to keep it on screen. */
    transform: translate3d(var(--ax), var(--ay), 0) translate(-50%, -50%);
    transition: transform 620ms var(--bb-ease-out-expo, ease);
  }

  /* Phones cannot fit a 120px blob beside a bubble: stack them and park the
     pair in the middle, so the walk becomes a vertical settle instead. */
  @media (max-width: 560px) {
    .pair,
    .pair.side-left {
      flex-direction: column;
      gap: 10px;
    }
  }

  .pair {
    display: flex;
    align-items: center;
    gap: 16px;
    flex-direction: row;
  }
  .pair.side-left {
    /* Bubble goes to the left of the blob: reverse the row instead of
       swapping which child is which. */
    flex-direction: row-reverse;
  }

  .blob-slot {
    flex: none;
    filter: drop-shadow(0 8px 20px rgba(0, 0, 0, 0.4));
  }

  .bubble {
    position: relative;
    width: min(380px, 82vw);
    background: var(--bb-surface, #17140f);
    border: 1px solid var(--bb-border-strong);
    border-radius: 14px 14px;
    padding: 18px;
    box-shadow: 0 16px 40px rgba(0, 0, 0, 0.45);
    animation: bubble-in 260ms var(--bb-ease-out-expo, ease) both;
  }
  @keyframes bubble-in {
    from { opacity: 0; transform: scale(0.96); }
    to { opacity: 1; transform: scale(1); }
  }

  /* Tail always points back at the blob, so it lives on whichever edge of
     the bubble faces the blob-slot flex sibling. */
  .tail {
    position: absolute;
    top: 44px;
    width: 14px;
    height: 14px;
    background: var(--bb-surface, #17140f);
    border-left: 1px solid var(--bb-border-strong);
    border-bottom: 1px solid var(--bb-border-strong);
    transform: rotate(45deg);
  }
  .side-right .tail { left: -8px; }
  .side-left .tail { right: -8px; transform: rotate(225deg); }

  .kicker {
    font-family: var(--bb-font-display); font-weight: 700; font-size: 11px;
    letter-spacing: 0.06em; text-transform: uppercase; color: var(--bb-tan);
    margin: 0 0 8px;
  }

  .intro {
    font-family: var(--bb-font-body); font-size: 13px; line-height: 1.55;
    color: var(--bb-muted); margin: 0 0 14px;
  }

  .step h4 {
    font-family: var(--bb-font-display); font-weight: 700; font-size: 16px;
    letter-spacing: -0.01em; color: var(--bb-white); margin: 0 0 8px;
  }
  .step-body {
    font-family: var(--bb-font-body); font-size: 13px; line-height: 1.55;
    color: var(--bb-muted); margin: 0;
  }

  .mod-cmd {
    display: flex; align-items: center; justify-content: space-between; gap: 10px;
    width: 100%; margin-top: 12px; padding: 10px 12px;
    background: rgba(0, 0, 0, 0.35);
    border: 1px dashed var(--bb-border-strong);
    border-radius: 8px 8px;
    cursor: pointer;
    transition: border-color 0.2s, background 0.2s;
  }
  .mod-cmd:hover { border-color: var(--bb-tan); background: rgba(201, 168, 124, 0.06); }
  .mod-cmd code { font-family: var(--bb-font-mono); font-size: 13px; color: var(--bb-tan-light); }
  .copy-hint {
    display: inline-flex; align-items: center; gap: 5px;
    font-family: var(--bb-font-body); font-weight: 600; font-size: 11.5px;
    color: var(--bb-muted);
  }
  .mod-cmd:hover .copy-hint { color: var(--bb-tan-pale); }

  .step-cta { display: inline-flex; margin-top: 12px; text-decoration: none; }

  .consent-check {
    display: flex; align-items: center; gap: 12px; margin-top: 4px;
    font-family: var(--bb-font-body); font-size: 13.5px; color: var(--bb-muted);
  }
  .consent-check :global(a) { color: var(--bb-tan-light); text-decoration: none; font-weight: 500; }
  .consent-check :global(a:hover) { text-decoration: underline; }

  .lang-row { margin-top: 4px; display: flex; }

  .pref-row {
    display: flex; align-items: center; justify-content: space-between; gap: 16px;
    margin-top: 14px; padding-top: 14px; border-top: 1px solid var(--bb-border);
  }
  .pref-name {
    font-family: var(--bb-font-body); font-weight: 600; font-size: 13.5px;
    color: var(--bb-white);
  }
  .pref-hint {
    font-family: var(--bb-font-body); font-size: 12px; line-height: 1.5;
    color: var(--bb-muted); margin: 4px 0 0;
  }

  .foot {
    display: flex; align-items: center; justify-content: space-between; gap: 12px;
    margin-top: 16px; padding-top: 14px; border-top: 1px solid var(--bb-border);
  }
  .dots { display: flex; gap: 7px; }
  .dot {
    width: 8px; height: 8px; border-radius: 50%; padding: 0;
    background: rgba(240, 236, 228, 0.18); border: none; cursor: pointer;
    transition: background var(--bb-dur-fast, 180ms) ease, transform var(--bb-dur-fast, 180ms) var(--bb-ease-out-back, ease);
  }
  .dot.on { background: var(--bb-tan); transform: scale(1.25); }
  .nav { display: flex; gap: 8px; }

  @media (prefers-reduced-motion: reduce) {
    /* Park the pair dead centre and drop the walking animation entirely;
       Bolota already refuses to mount its own engine in this mode. */
    .guide-wrap {
      transition: none;
      transform: translate3d(50vw, 50vh, 0) translate(-50%, -50%);
    }
    .bubble { animation: none; }
  }
</style>
