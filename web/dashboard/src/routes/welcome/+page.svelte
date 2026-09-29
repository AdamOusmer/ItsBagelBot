<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount, tick } from 'svelte';
  import { fly } from 'svelte/transition';
  import { MediaQuery } from 'svelte/reactivity';
  import { page } from '$app/state';
  import { goto, onNavigate, pushState, replaceState } from '$app/navigation';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { bezier } from '@bagel/ui/lib/tween';
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import { decode, parallax } from '@bagel/ui/svelte/actions';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import CopySurface from '@bagel/ui/svelte/CopySurface.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import type { IconName } from '@bagel/ui/lib/icons';
  import LanguageSwitcher from '@bagel/ui/svelte/LanguageSwitcher.svelte';
  import RadioGroup from '@bagel/ui/svelte/RadioGroup.svelte';
  import Stepper from '@bagel/ui/svelte/Stepper.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Toggle from '@bagel/ui/svelte/Toggle.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { LOCALES, ensureCatalog, translate, type Locale } from '@bagel/kit/i18n';
  import CursorSwitch from '$lib/components/CursorSwitch.svelte';
  import Sky from '@bagel/ui/svelte/Sky.svelte';
  import Completion from '$lib/components/welcome/Completion.svelte';
  import ImportWizard from '$lib/components/welcome/ImportWizard.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';

  let { data } = $props();

  let locale = $state<Locale>(getI18n().locale);
  const tr = (key: string, params?: Record<string, string | number>) => translate(locale, key, params);

  type Kind = 'hero' | 'consent' | 'mod' | 'choice' | 'lang' | 'commands' | 'modules';
  type Setup = 'start-new' | 'integrate' | 'quiet' | 'import';
  type Beat = {
    kind: Kind;
    face: string;
    title: string;
    body: string;
  };

  const MOD_COMMAND = '/mod ItsBagelBot';

  const INTRO: Beat[] = [
    { kind: 'consent', face: 'attentive', title: 'consentTitle', body: 'consentBody' },
    { kind: 'mod', face: 'attentive', title: 'step1Title', body: 'step1Body' },
    { kind: 'lang', face: 'curious', title: 'langTitle', body: 'langBody' },
    { kind: 'choice', face: 'curious', title: 'choiceTitle', body: 'choiceBody' }
  ];
  const NEW_STREAMER: Beat[] = [
    { kind: 'commands', face: 'excited', title: 'newCommandsTitle', body: 'newCommandsBody' },
    { kind: 'modules', face: 'proud', title: 'newModulesTitle', body: 'newModulesBody' }
  ];
  const consentStep = 1;
  const choiceStep = 1 + INTRO.findIndex((b) => b.kind === 'choice');
  let setup = $state<Setup>(page.url.searchParams.get('import') === '1' ? 'import' : 'start-new');
  let hoveredChoice = $state<Setup | null>(null);

  const CHOICES = [
    { id: 'start-new', key: 'New', icon: 'start', face: 'excited' },
    { id: 'integrate', key: 'Integrate', icon: 'integrate', face: 'attentive' },
    { id: 'quiet', key: 'Quiet', icon: 'quiet', face: 'sleepy' },
    { id: 'import', key: 'Import', icon: 'importFile', face: 'curious' }
  ] as const satisfies readonly { id: Setup; key: string; icon: IconName; face: string }[];
  const choiceOf = (id: string) => CHOICES.find((c) => c.id === id) ?? CHOICES[0];
  const choiceOptions = $derived(
    CHOICES.map((c) => ({
      value: c.id,
      label: tr(`onboarding.choice${c.key}Title`),
      description: tr(`onboarding.choice${c.key}Body`)
    }))
  );
  const langOptions = $derived(LOCALES.map((l) => ({ code: l, label: l.toUpperCase(), current: l === locale })));
  const choiceIndex = $derived(Math.max(0, CHOICES.findIndex((c) => c.id === (hoveredChoice ?? setup))));
  const choicePosition = $derived(['12.5%', '37.5%', '62.5%', '87.5%'][choiceIndex]);
  const choicePositionCompact = $derived(['25%', '75%', '25%', '75%'][choiceIndex]);
  const compactChoices = new MediaQuery('max-width: 560px');
  function pickSetup(id: string) {
    const choice = choiceOf(id);
    if (setup === choice.id) return;
    setup = choice.id;
    react(choice.face, 1400);
  }
  function previewChoice(target: EventTarget | null) {
    const input = target instanceof Element ? target.closest('label')?.querySelector('input') : null;
    const choice = CHOICES.find((c) => c.id === input?.value);
    if (!choice) return;
    hover = choice.face;
    hoveredChoice = choice.id;
  }
  function clearChoice() {
    hover = null;
    hoveredChoice = null;
  }

  const tour = $derived<Beat[]>(
    [...INTRO, ...(setup === 'start-new' ? NEW_STREAMER : [])].map((b) => ({
      kind: b.kind,
      face: b.face,
      title: tr(`onboarding.${b.title}`),
      body: tr(`onboarding.${b.body}`)
    }))
  );
  const importStageKeys = [
    'onboardingImport.stagePick',
    'onboardingImport.stageConnect',
    'onboardingImport.stageCommands',
    'onboardingImport.stageExtras',
    'onboardingImport.stageReview',
    'onboardingImport.stageDone'
  ] as const;
  const railSteps = $derived([
    ...tour.map((s) => ({ label: s.title })),
    ...(setup === 'import' ? importStageKeys.map((key) => ({ label: tr(key) })) : [])
  ]);
  const journeyTotal = $derived(railSteps.length);
  const steps = $derived<Beat[]>([
    {
      kind: 'hero',
      face: 'happy',
      title: tr('onboarding.heroTitle', { name: data.name }),
      body: tr('onboarding.heroBody')
    },
    ...tour
  ]);

  const importRequested = page.url.searchParams.get('import') === '1';
  let importLink = $state(importRequested);
  const importActive = $derived(page.state.importing ?? importLink);
  let step = $state(importRequested ? choiceStep : 0);
  let furthest = $state(importRequested ? choiceStep : 0);
  let dir = $state(1);
  let consentAccepted = $state(false);
  let leaving = $state(false);
  let showCompletion = $state(false);
  let saveError = $state(false);

  const current = $derived(steps[step]);
  const last = $derived(step === steps.length - 1);
  const consentBlocked = $derived(!consentAccepted);
  const maxStep = $derived(consentBlocked ? consentStep : Math.min(furthest + 1, steps.length - 1));
  const nextDisabled = $derived(step + 1 > maxStep);
  const canAdvance = $derived(!nextDisabled && !last);
  const blobRight = $derived(step % 2 === 1);
  const progress = $derived(
    setup === 'import'
      ? Math.max(step - 1, 0) / Math.max(journeyTotal - 1, 1)
      : step / Math.max(steps.length - 1, 1)
  );

  const expo = bezier(0.16, 1, 0.3, 1);
  const TRAVEL = 72;
  const sideOf = (i: number) => (i % 2 === 0 ? 1 : -1);
  let instant = false;
  const arrive = (node: Element, { i = 0 }: { i?: number } = {}) =>
    prefersReducedMotion() || instant
      ? { duration: 0 }
      : fly(node, { x: dir * TRAVEL * sideOf(i), duration: 760, delay: 160 + i * 70, easing: expo, opacity: 0 });
  const depart = (node: Element, { i = 0 }: { i?: number } = {}) =>
    prefersReducedMotion() || departed || instant
      ? { duration: 0 }
      : fly(node, { x: -dir * 56 * sideOf(i), duration: 380, delay: i * 35, easing: expo, opacity: 0 });

  const CLEAR_MS = 300;
  let clearing = $state(false);
  let departed = false;
  const isChoice = (i: number) => steps[i]?.kind === 'choice';

  type Sequence = 'entrance' | 'burst' | 'orbit' | 'comet';
  let sequence = $state<Sequence>('entrance');
  let sequenceKey = $state(0);
  function play(seq: Sequence) {
    sequence = seq;
    sequenceKey += 1;
  }

  let reaction = $state<string | null>(null);
  let hover = $state<string | null>(null);
  let reactionTimer: ReturnType<typeof setTimeout> | null = null;
  const expression = $derived(reaction ?? hover ?? current.face);
  function react(face: string, ms: number) {
    reaction = face;
    if (reactionTimer) clearTimeout(reactionTimer);
    reactionTimer = setTimeout(() => (reaction = null), ms);
  }
  function reactionFor(target: EventTarget | null): string | null {
    const el = target instanceof Element ? target.closest<HTMLElement>('button, a') : null;
    if (!el || el.hasAttribute('disabled')) return null;
    return el.dataset.react ?? 'curious';
  }

  let px = $state(0);
  let py = $state(0);
  function follow(point: { px: number; py: number }) {
    px = point.px;
    py = point.py;
  }

  function go(i: number) {
    const target = Math.max(0, Math.min(i, maxStep));
    if (target === step || leaving || clearing) return;
    dir = target > step ? 1 : -1;
    hover = null;
    if (isChoice(target) !== isChoice(step) && !prefersReducedMotion()) {
      clearing = true;
      setTimeout(() => land(target), CLEAR_MS);
      return;
    }
    land(target);
  }
  function land(target: number) {
    departed = clearing;
    clearing = false;
    step = target;
    tick().then(() => (departed = false));
    furthest = Math.max(furthest, target);
    play('entrance');
  }
  const goNext = () => go(step + 1);
  const goBack = () => go(step - 1);
  const goTo = (i: number) => go(i + 1);

  const CONSENT_KEY = 'bb-welcome-consent';
  const TOUR_KEY = 'bb-welcome-tour';
  const SETUPS: readonly Setup[] = CHOICES.map((c) => c.id);
  let tourReady = false;
  let tourDone = false;

  type SavedTour = { step: number; furthest: number; setup: Setup; locale: Locale };

  function readTour(): SavedTour | null {
    try {
      const v = JSON.parse(sessionStorage.getItem(TOUR_KEY) ?? 'null') as Partial<SavedTour> | null;
      if (!v || !SETUPS.includes(v.setup as Setup) || !LOCALES.includes(v.locale as Locale)) return null;
      if (!Number.isInteger(v.step) || !Number.isInteger(v.furthest)) return null;
      return v as SavedTour;
    } catch {
      return null;
    }
  }

  function restoreTour(saved: SavedTour) {
    setup = saved.setup;
    const top = INTRO.length + (saved.setup === 'start-new' ? NEW_STREAMER.length : 0);
    const cap = consentAccepted ? top : Math.min(consentStep, top);
    step = Math.max(0, Math.min(saved.step, cap));
    furthest = Math.max(step, Math.min(saved.furthest, cap));
    if (saved.locale !== locale) void applyLocale(saved.locale);
  }

  async function applyLocale(l: Locale) {
    await ensureCatalog(l);
    locale = l;
    document.documentElement.lang = l;
  }

  function saveTour() {
    try {
      sessionStorage.setItem(TOUR_KEY, JSON.stringify({ step, furthest, setup, locale }));
    } catch {
    }
  }

  function clearTour() {
    tourDone = true;
    try {
      sessionStorage.removeItem(TOUR_KEY);
    } catch {
    }
  }

  $effect(() => {
    step;
    furthest;
    setup;
    locale;
    if (tourReady && !tourDone) saveTour();
  });

  onMount(() => {
    try {
      consentAccepted = sessionStorage.getItem(CONSENT_KEY) === '1';
      if (importRequested && !consentAccepted) {
        importLink = false;
        replaceState('/welcome', {});
        step = consentStep;
      } else if (!importRequested) {
        const saved = readTour();
        if (saved) {
          instant = true;
          restoreTour(saved);
          requestAnimationFrame(() => requestAnimationFrame(() => (instant = false)));
        }
      }
    } catch {
    }
    tourReady = true;
  });

  function startImport() {
    if (leaving || !consentAccepted || step < choiceStep) return;
    setup = 'import';
    pushState('/welcome?import=1', { importing: true });
  }

  function returnFromImport(welcomeStep: number) {
    importLink = false;
    replaceState('/welcome', {});
    setup = 'import';
    step = Math.max(consentStep, Math.min(welcomeStep, choiceStep));
    furthest = Math.max(furthest, step);
    dir = -1;
    play('entrance');
  }

  async function setLang(l: Locale) {
    if (l === locale) return;
    await applyLocale(l);
    react('happy', 1400);
    const body = new FormData();
    body.set('to', l);
    body.set('next', '/welcome');
    fetch('/lang', { method: 'POST', body }).catch(() => {});
  }

  function onConsent(on: boolean) {
    try {
      if (on) sessionStorage.setItem(CONSENT_KEY, '1');
      else sessionStorage.removeItem(CONSENT_KEY);
    } catch {
    }
    if (on) react('excited', 1600);
  }

  function celebrateCopy(ok: boolean) {
    if (!ok) return;
    react('proud', 2200);
    play('entrance');
  }

  let form = $state<HTMLFormElement | null>(null);
  let destination = $state('/');
  const EXIT_MS = 640;
  function finish() {
    if (leaving || !consentAccepted || setup === 'import' || !last || step < choiceStep) return;
    saveError = false;
    leaving = true;
    hover = null;
    react('proud', 4000);
    play('burst');
    setTimeout(() => form?.requestSubmit(), prefersReducedMotion() ? 0 : EXIT_MS);
  }
  const bootLocale = getI18n().locale;
  const VEIL_MS = 420;
  let veiled = $state(false);
  function exitTo(to: string) {
    clearTour();
    if (locale === bootLocale) {
      goto(to);
      return;
    }
    veiled = true;
    setTimeout(() => location.assign(to), prefersReducedMotion() ? 0 : VEIL_MS);
  }
  onNavigate((navigation) => {
    if (!document.startViewTransition || prefersReducedMotion()) return;
    return new Promise((resolve) => {
      document.startViewTransition(async () => {
        resolve();
        await navigation.complete;
      });
    });
  });

  const submit: SubmitFunction = () => async ({ result }) => {
    if (result.type === 'redirect') {
      clearTour();
      destination = result.location;
      showCompletion = true;
      return;
    }
    leaving = false;
    saveError = true;
    react('attentive', 2400);
  };

  const ARROW_BLOCKERS =
    'input, textarea, select, [contenteditable=""], [contenteditable="true"], [role="group"]:not([data-arrows-ok]), [role="radiogroup"], [role="tablist"]';
  const MODIFIER_KEYS = ['metaKey', 'ctrlKey', 'altKey', 'shiftKey'] as const;

  function arrowsBlocked(e: KeyboardEvent): boolean {
    if (MODIFIER_KEYS.some((key) => e[key])) return true;
    return e.target instanceof Element && e.target.closest(ARROW_BLOCKERS) !== null;
  }

  const keysPaused = (e: KeyboardEvent) => leaving || importActive || arrowsBlocked(e);
  function onKeydown(e: KeyboardEvent) {
    if (keysPaused(e)) return;
    if (e.key === 'ArrowRight' && canAdvance) {
      e.preventDefault();
      goNext();
    } else if (e.key === 'ArrowLeft' && step > 0) {
      e.preventDefault();
      goBack();
    }
  }

  let headingEl = $state<HTMLHeadingElement | null>(null);
  let settled = false;
  $effect(() => {
    step;
    if (settled) headingEl?.focus({ preventScroll: true });
    settled = true;
  });
</script>

<svelte:head>
  <title>{tr('onboarding.title')} · ItsBagelBot</title>
  <meta name="robots" content="noindex, nofollow" />
</svelte:head>

<svelte:window onkeydown={onKeydown} />

{#if importActive}
  <ImportWizard onback={returnFromImport} onexit={exitTo} {consentAccepted} {locale} />
{:else}
<Sky shift={blobRight ? 1 : -1} turn={step * 24} {px} {py} {progress} {leaving} />

<div class="welcome" class:leaving data-orbs="off" use:parallax={{ scope: 'viewport', onmove: follow }}>
  <header class="top">
    <Brand title="ItsBagelBot" sub={tr('common.console')} logoSrc="/logo.png" logoAlt="" size="md" />
    <Stepper
      compact
      steps={railSteps}
      current={step - 1}
      maxStep={maxStep - 1}
      label={tr('onboarding.stepOf', { n: Math.max(step, 1), total: journeyTotal })}
      onselect={goTo}
    />
  </header>

  <main class="stage">
    <div class="pair" class:choice-stage={current.kind === 'choice'} class:clearing style="--choice-position: {choicePosition}; --choice-position-compact: {choicePositionCompact}; --dir: {dir};">
      <div class="blob-col" class:right={blobRight} class:hero={step === 0}>
        <div class="scale">
          <div class="float">
            <div class="plate" aria-hidden="true"></div>
            <Bolota
              name={data.name}
              size={240}
              active
              follow
              cycle={false}
              {expression}
              {sequence}
              {sequenceKey}
              sequenceFor={1100}
              sequenceHold={240}
            />
          </div>
        </div>
      </div>

      <div class="copy-col" class:left={blobRight}>
        {#key step}
          <section class="step" aria-labelledby="wlc-title">
            <div class="kicker" in:arrive={{ i: 0 }} out:depart={{ i: 0 }}>
              <Eyebrow as="p">{step === 0 ? tr('onboarding.title') : tr('onboarding.stepOf', { n: step, total: journeyTotal })}</Eyebrow>
            </div>
            <h1
              id="wlc-title"
              class="title"
              class:hero={step === 0}
              tabindex="-1"
              bind:this={headingEl}
              in:arrive={{ i: 1 }}
              out:depart={{ i: 1 }}
            >
              {current.title}
            </h1>
            <div class="body" in:arrive={{ i: 2 }} out:depart={{ i: 2 }}><Text size="lg" tone="muted">{current.body}</Text></div>

            {#if current.kind === 'consent'}
              <div class="control consent" in:arrive|global={{ i: 3 }} out:depart|global={{ i: 3 }}>
                <Toggle bind:on={consentAccepted} onchange={onConsent} />
                <Text as="span" size="sm" tone="muted" class="bb-prose">{@html tr('onboarding.consentLabel')}</Text>
              </div>
              <div class="consent-hint" class:shown={consentBlocked}>
                <Text size="xs" tone="muted" id="wlc-consent-hint">{tr('onboarding.consentHint')}</Text>
              </div>
            {:else if current.kind === 'choice'}
              <div class="control role-choices" in:arrive|global={{ i: 3 }} out:depart|global={{ i: 3 }}>
                <RadioGroup
                  variant="cards"
                  cols={compactChoices.current ? 2 : 4}
                  name="setup"
                  label={tr('onboarding.choiceTitle')}
                  value={setup}
                  options={choiceOptions}
                  onchange={pickSetup}
                  aria-describedby="wlc-preview-{setup}"
                  onpointerover={(e: PointerEvent) => previewChoice(e.target)}
                  onfocusin={(e: FocusEvent) => previewChoice(e.target)}
                  onpointerleave={clearChoice}
                  onfocusout={clearChoice}
                >
                  {#snippet lead(option, on)}
                    <span class="role-glyph" class:on aria-hidden="true"><Icon name={choiceOf(option.value).icon} size={15} /></span>
                  {/snippet}
                </RadioGroup>
                <div class="role-previews">
                  {#each CHOICES as choice (choice.id)}
                    <div class="role-preview" class:shown={setup === choice.id}>
                      <Text size="sm" tone="muted" id="wlc-preview-{choice.id}">{tr(`onboarding.choice${choice.key}Preview`)}</Text>
                    </div>
                  {/each}
                </div>
              </div>
            {:else if current.kind === 'lang'}
              <div class="control prefs" in:arrive|global={{ i: 3 }} out:depart|global={{ i: 3 }}>
                <div class="lang-row">
                  <LanguageSwitcher ariaLabel={tr('lang.switchAria')} options={langOptions} onselect={(code) => setLang(code as Locale)} />
                </div>
                <div class="pref-row">
                  <div class="pref-text">
                    <Text as="span" size="sm"><strong>{tr('settings.customCursor')}</strong></Text>
                    <Text size="xs" tone="muted" id="wlc-cursor-hint">{tr('settings.customCursorHint')}</Text>
                  </div>
                  <CursorSwitch describedby="wlc-cursor-hint" />
                </div>
              </div>
            {:else if current.kind === 'mod'}
              <div class="control mod-well" use:decode in:arrive|global={{ i: 3 }} out:depart|global={{ i: 3 }}>
                <CopySurface
                  variant="well"
                  text={MOD_COMMAND}
                  hint={tr('common.copy')}
                  copiedLabel={tr('common.copied')}
                  flashMs={2000}
                  legacyFallback
                  title={tr('common.copy')}
                  oncopy={celebrateCopy}
                >
                  {#snippet children()}<span data-decode={MOD_COMMAND}>{MOD_COMMAND}</span>{/snippet}
                </CopySurface>
              </div>
            {:else if current.kind === 'commands'}
              <div class="control rehearsal-example" in:arrive|global={{ i: 3 }} out:depart|global={{ i: 3 }}>
                <ChatPreview name="hello" response={tr('onboarding.exampleReply')} tag={tr('onboarding.exampleLabel')} broadcasterName={data.name} {locale} samples={{ user: tr('onboarding.exampleViewer') }} />
                <Text size="xs" tone="muted">{tr('onboarding.newCommandsHint')}</Text>
              </div>
            {:else if current.kind === 'modules'}
              <div class="control rehearsal-example" in:arrive|global={{ i: 3 }} out:depart|global={{ i: 3 }}>
                <ChatPreview kind="reply" name="welcome" viewerText={tr('onboarding.moduleExampleViewer')} response={tr('onboarding.moduleExampleReply')} tag={tr('onboarding.moduleExampleLabel')} broadcasterName={data.name} {locale} samples={{ user: tr('onboarding.exampleViewer') }} />
                <Text size="xs" tone="muted">{tr('onboarding.newModulesHint')}</Text>
              </div>
            {/if}

            <div
              class="actions"
              role="group"
              data-arrows-ok
              in:arrive={{ i: 4 }}
              out:depart={{ i: 4 }}
              onpointerover={(e) => (hover = reactionFor(e.target))}
              onpointerout={() => (hover = null)}
            >
              {#if current.kind === 'hero'}
                <Button data-react="excited" onclick={goNext}>{tr('onboarding.heroCta')}</Button>
              {:else}
                {#if last}
                  {#if setup === 'import'}
                    <Button data-react="excited" onclick={startImport} tone="success">{tr('onboarding.choiceImportCta')}</Button>
                  {:else}
                    <Button data-react="excited" onclick={finish} tone="success">{tr('onboarding.finishCta')}</Button>
                  {/if}
                {:else}
                  <Button
                    data-react="excited"
                    onclick={goNext}
                    disabled={nextDisabled}
                    aria-describedby={current.kind === 'consent' ? 'wlc-consent-hint' : undefined}
                  >{tr('onboarding.next')}</Button>
                {/if}
                <Button variant="ghost" onclick={goBack}>{tr('onboarding.back')}</Button>
              {/if}
            </div>
            {#if saveError}
              <div class="save-error"><AlertBanner tone="danger">{tr('onboarding.saveError')}</AlertBanner></div>
            {/if}
          </section>
        {/key}
      </div>
    </div>
  </main>

  <footer class="foot">
    <div class="line" aria-hidden="true" style="--p: {progress};">
      <span class="fill"></span>
      <span class="bead-track"><span class="bead"></span></span>
    </div>
    <div class="meta"></div>
  </footer>
</div>

{#if showCompletion}
  <Completion label={tr('onboarding.done')} oncomplete={() => exitTo(destination)} />
{/if}

<form method="POST" action="?/done" use:enhance={submit} bind:this={form} hidden>
  <input type="hidden" name="preset" value={setup} />
  <input type="hidden" name="consent" value={consentAccepted ? 'yes' : 'no'} />
</form>
{/if}

{#if veiled}<div class="veil" aria-hidden="true"></div>{/if}

<style>
  .welcome {
    --gutter: clamp(20px, 4vw, 48px);
    --gap: clamp(24px, 5vw, 72px);
    position: relative;
    z-index: 1;
    min-height: 100svh;
    display: grid;
    grid-template-rows: auto 1fr auto;
    overflow-x: clip;
  }

  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px 24px;
    flex-wrap: wrap;
    padding: 22px var(--gutter) 0;
    animation: settle calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo) backwards;
  }

  .stage {
    display: grid;
    place-items: center;
    padding: 24px var(--gutter);
  }

  .pair {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: var(--gap);
    align-items: start;
    width: min(100%, 1120px);
    min-height: clamp(440px, 64svh, 720px);
    transition:
      transform var(--bb-dur-slow) var(--bb-ease-out-expo),
      opacity var(--bb-dur-slow) var(--bb-ease-out-expo);
  }

  .blob-col,
  .copy-col {
    transition:
      transform calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo),
      opacity var(--bb-dur-slow) var(--bb-ease-out-expo);
    will-change: transform;
  }
  .blob-col.right { transform: translateX(calc(100% + var(--gap))); }
  .copy-col.left { transform: translateX(calc(-100% - var(--gap))); }

  .step {
    transition:
      transform var(--bb-dur-base) var(--bb-ease-out-expo),
      opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .clearing .blob-col { opacity: 0; transition-duration: calc(var(--bb-dur-slow) * 1.5), var(--bb-dur-fast); }
  .clearing .step {
    opacity: 0;
    transform: translateX(calc(var(--dir) * -56px));
  }

  .blob-col {
    --blob-scale: 0.86;
    display: flex;
    justify-content: center;
    align-self: center;
  }
  .blob-col.hero { --blob-scale: 1.1; }

  .scale {
    transform: scale(var(--blob-scale));
    transition: transform calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo);
  }

  .float {
    position: relative;
    filter: drop-shadow(0 18px 34px rgba(var(--bb-shadow-rgb), 0.42));
    animation: float 9s ease-in-out infinite;
  }

  .plate {
    position: absolute;
    inset: -22%;
    border-radius: 50%;
    background: radial-gradient(
      circle at 40% 35%,
      rgba(var(--bb-green-glow-rgb), 0.34),
      rgba(var(--bb-tan-rgb), 0.16) 46%,
      transparent 70%
    );
    filter: blur(22px);
    animation: breathe 6s ease-in-out infinite;
    z-index: -1;
  }

  .copy-col {
    display: grid;
    max-width: 560px;
    justify-self: start;
    padding-top: clamp(16px, 7svh, 72px);
  }
  .copy-col > .step { grid-area: 1 / 1; }

  .pair.choice-stage { grid-template-columns: minmax(0, 1fr); gap: 0; }
  .choice-stage .blob-col,
  .choice-stage .blob-col.right {
    grid-column: 1;
    grid-row: 1;
    position: relative;
    justify-self: stretch;
    align-self: start;
    height: 128px;
    transform: none;
  }
  .choice-stage .blob-col .scale {
    --blob-scale: .46;
    position: absolute;
    top: -50px;
    left: calc(var(--choice-position) - 120px);
    transition: left var(--bb-dur-slow) var(--bb-ease-out-expo), transform calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo);
  }
  .choice-stage .copy-col,
  .choice-stage .copy-col.left {
    grid-column: 1;
    grid-row: 2;
    width: 100%;
    max-width: none;
    padding-top: 0;
    transform: none;
  }
  .choice-stage .step { text-align: center; }
  .choice-stage .body { max-width: none; margin-inline: auto; }
  .choice-stage .actions { justify-content: center; }

  .kicker { margin-bottom: 14px; }

  .title {
    margin: 0 0 18px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: clamp(2rem, 4.4vw, 3.3rem);
    line-height: 1.02;
    letter-spacing: -0.03em;
    color: var(--bb-white);
    background: linear-gradient(100deg, var(--bb-white) 0 40%, var(--bb-tan-pale) 50%, var(--bb-white) 60% 100%);
    background-size: 260% 100%;
    background-position: 120% 0;
    -webkit-background-clip: text;
    background-clip: text;
    -webkit-text-fill-color: transparent;
    animation: sheen 1.8s var(--bb-ease-out-expo) 520ms both;
    outline: none;
  }
  .title.hero { font-size: clamp(2.6rem, 6.2vw, 4.6rem); }

  .body { max-width: 46ch; }

  .control { margin-top: 24px; }

  .role-choices { --choice-min-h: 132px; width: 100%; }
  .role-previews { display: grid; margin-top: 14px; }
  .role-preview {
    grid-area: 1 / 1;
    max-width: 62ch;
    justify-self: center;
    visibility: hidden;
    opacity: 0;
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo), visibility 0s linear var(--bb-dur-base);
  }
  .role-preview.shown {
    visibility: visible;
    opacity: 1;
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .role-glyph {
    display: inline-grid;
    place-items: center;
    flex: none;
    width: 32px;
    height: 32px;
    border-radius: var(--bb-radius-sm);
    border: 1px solid var(--bb-border-strong);
    color: var(--bb-tan-pale);
    transition:
      color var(--bb-dur-fast) ease,
      border-color var(--bb-dur-fast) ease;
  }
  .role-glyph.on {
    color: var(--bb-green-glow);
    border-color: rgba(var(--bb-green-glow-rgb), 0.5);
  }
  .rehearsal-example { display: grid; gap: 12px; width: min(100%, 480px); text-align: left; }

  .consent {
    display: flex;
    align-items: center;
    gap: 14px;
  }

  .consent-hint {
    margin-top: 8px;
    min-height: 20px;
    visibility: hidden;
    opacity: 0;
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .consent-hint.shown { visibility: visible; opacity: 1; }

  .lang-row { display: flex; }

  .pref-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    margin-top: 16px;
    padding-top: 16px;
    border-top: 1px solid var(--bb-border);
  }
  .pref-text { display: grid; gap: 4px; }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px 14px;
    margin-top: 28px;
  }

  .save-error { margin-top: 12px; }

  .foot {
    display: grid;
    gap: 10px;
    padding: 0 var(--gutter) 18px;
    animation: settle calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo) 120ms backwards;
  }
  .meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 36px;
  }
  .line {
    position: relative;
    height: 1px;
    background: var(--bb-border);
  }
  .fill {
    position: absolute;
    inset: 0;
    transform-origin: left;
    transform: scaleX(var(--p));
    background: linear-gradient(90deg, var(--bb-tan), var(--bb-green-glow));
    transition: transform calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo);
  }
  .bead-track {
    position: absolute;
    inset: 0;
    transform: translateX(calc(var(--p) * 100%));
    transition: transform calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo);
  }
  .bead {
    position: absolute;
    top: -3px;
    left: -3.5px;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--bb-green-glow);
    box-shadow: 0 0 0 3px rgba(var(--bb-green-glow-rgb), .16), 0 0 14px rgba(var(--bb-green-glow-rgb), .7);
    animation: breathe 3s ease-in-out infinite;
  }

  .leaving .pair {
    transform: translateX(-6vw);
    opacity: 0;
  }
  .leaving .top,
  .leaving .foot {
    opacity: 0;
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .veil {
    position: fixed;
    inset: 0;
    z-index: var(--bb-z-overlay);
    background: var(--bb-bg-0);
    animation: veil-in var(--bb-dur-base) var(--bb-ease-out-expo) both;
  }
  @keyframes veil-in { from { opacity: 0; } }
  :global(::view-transition-old(root)),
  :global(::view-transition-new(root)) {
    animation-duration: var(--bb-dur-slow);
    animation-timing-function: var(--bb-ease-out-expo);
  }

  @keyframes settle {
    from { opacity: 0; transform: translateX(-16px); }
    to { opacity: 1; transform: none; }
  }
  @keyframes float {
    0%, 100% { transform: translate(0, 0); }
    32% { transform: translate(14px, -6px); }
    64% { transform: translate(-11px, 5px); }
  }
  @keyframes breathe {
    0%, 100% { opacity: 0.7; transform: scale(1); }
    50% { opacity: 1; transform: scale(1.08); }
  }
  @keyframes sheen {
    from { background-position: 120% 0; }
    to { background-position: -40% 0; }
  }

  @media (max-width: 760px) {
    .pair {
      grid-template-columns: 1fr;
      gap: 8px;
      justify-items: center;
      text-align: center;
    }
    .blob-col.right,
    .copy-col.left { transform: none; }
    .stage { align-items: start; }
    .pair {
      min-height: 0;
      align-items: start;
    }
    .copy-col { padding-top: 0; }
    .blob-col,
    .blob-col.hero {
      --blob-scale: 0.66;
      height: 176px;
      align-items: flex-start;
      align-self: start;
    }
    .scale { transform-origin: top center; }
    .copy-col {
      max-width: 100%;
      justify-self: center;
    }
    .body { margin-inline: auto; }
    .consent { text-align: left; }
    .pref-row { text-align: left; }
    .actions { justify-content: center; }
    .mod-well { display: flex; justify-content: center; }
    .role-choices, .rehearsal-example { margin-inline: auto; }
    .role-previews { text-align: left; }
  }

  @media (max-width: 560px) {
    .role-choices { --choice-min-h: 148px; }
    .choice-stage .blob-col .scale { left: calc(var(--choice-position-compact) - 120px); }
  }

  @media (prefers-reduced-motion: reduce) {
    .pair,
    .blob-col,
    .copy-col,
    .step,
    .scale,
    .fill,
    .role-preview,
    .consent-hint,
    .role-glyph,
    .choice-stage .blob-col .scale,
    .bead-track { transition: none; }
    .top,
    .foot,
    .float,
    .plate,
    .bead,
    .veil,
    .title { animation: none; }
    .title {
      background: none;
      -webkit-text-fill-color: currentColor;
    }
  }
</style>
