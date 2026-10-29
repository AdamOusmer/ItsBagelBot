<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/kit/styles/ui-console.css';
  import { onMount, untrack } from 'svelte';
  import { MediaQuery } from 'svelte/reactivity';
  import { page } from '$app/state';
  import { goto, onNavigate, pushState, replaceState } from '$app/navigation';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import { decode } from '@bagel/ui/svelte/actions';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import CopySurface from '@bagel/ui/svelte/CopySurface.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import type { IconName } from '@bagel/ui/lib/icons';
  import LanguageSwitcher from '@bagel/ui/svelte/LanguageSwitcher.svelte';
  import RadioGroup from '@bagel/ui/svelte/RadioGroup.svelte';
  import SetupStage from '@bagel/ui/svelte/SetupStage.svelte';
  import SetupStep from '@bagel/ui/svelte/SetupStep.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { LOCALES, ensureCatalog, translate, type Locale } from '@bagel/kit/i18n';
  import { localeFlag } from '@bagel/kit/site-links';
  import CursorSwitch from '$lib/components/CursorSwitch.svelte';
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
  const langOptions = $derived(
    LOCALES.map((code) => ({
      code,
      label: code.toUpperCase(),
      title: page.data.localeNames?.[code] ?? code,
      flag: localeFlag(code),
      current: code === locale
    }))
  );
  const choiceIndex = $derived(Math.max(0, CHOICES.findIndex((c) => c.id === (hoveredChoice ?? setup))));
  const choicePosition = $derived(['12.5%', '37.5%', '62.5%', '87.5%'][choiceIndex]);
  const choicePositionCompact = $derived(['25%', '75%', '25%', '75%'][choiceIndex]);
  const compactChoices = new MediaQuery('max-width: 560px');
  const terseChoices = new MediaQuery('(max-width: 560px), (max-height: 700px)');
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
  type Phase = 'idle' | 'leaving';
  let phase = $state<Phase>('idle');
  const leaving = $derived(phase === 'leaving');
  const locked = $derived(leaving);
  let showCompletion = $state(false);
  let saveError = $state(false);
  let saveErrorServer = $state('');

  const current = $derived(steps[step]);
  const last = $derived(step === steps.length - 1);
  const consentBlocked = $derived(!consentAccepted);
  const maxStep = $derived(consentBlocked ? consentStep : Math.min(furthest + 1, steps.length - 1));
  const nextDisabled = $derived(step + 1 > maxStep);
  const canAdvance = $derived(!nextDisabled && !last);
  const pastChoice = $derived(consentAccepted && step >= choiceStep);
  const canImport = $derived(!leaving && pastChoice);
  const canFinish = $derived(!locked && pastChoice && last && setup !== 'import');
  let instant = $state(false);

  const SWAP_MS = 280;
  let choiceLayout = $state(false);
  $effect(() => {
    const next = current.kind === 'choice';
    if (next === untrack(() => choiceLayout)) return;
    const timer = setTimeout(() => (choiceLayout = next), instant || prefersReducedMotion() ? 0 : SWAP_MS);
    return () => clearTimeout(timer);
  });

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

  function go(i: number) {
    const target = Math.max(0, Math.min(i, maxStep));
    if (target === step || locked) return;
    dir = target > step ? 1 : -1;
    hover = null;
    step = target;
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
    if (!canImport) return;
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
    if (!canFinish) return;
    saveError = false;
    saveErrorServer = '';
    phase = 'leaving';
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
    phase = 'idle';
    saveErrorServer = result.type === 'failure' && result.status < 500 && typeof result.data?.error === 'string' ? result.data.error : '';
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
<SetupStage
  label={tr('onboarding.title')}
  steps={railSteps}
  current={step - 1}
  maxStep={maxStep - 1}
  stepLabel={tr('onboarding.stepOf', { n: Math.max(step, 1), total: journeyTotal })}
  onSelect={goTo}
  hero={step === 0}
  wide={choiceLayout}
  {leaving}
  style={choiceLayout ? `--setup-art-x: calc(${compactChoices.current ? choicePositionCompact : choicePosition} - 50%)` : undefined}
  data-orbs="off"
>
  {#snippet start()}<Brand title="ItsBagelBot" sub={tr('common.console')} logoSrc="/logo.png" logoAlt="" size="md" />{/snippet}
  {#snippet art()}
    <Bolota name={data.name} size={240} active follow cycle={false} {expression} {sequence} {sequenceKey} sequenceFor={1100} sequenceHold={240} />
  {/snippet}
  {#key step}
    <SetupStep
      kicker={step === 0 ? tr('onboarding.title') : tr('onboarding.stepOf', { n: step, total: journeyTotal })}
      title={current.title}
      body={current.body}
      {dir}
      {instant}
      hero={step === 0}
      bind:heading={headingEl}
    >
      {#if current.kind === 'consent'}
        <div class="consent">
          <Switch bind:checked={consentAccepted} label={current.title} onCheckedChange={onConsent} />
          <Text as="span" size="sm" tone="muted" class="bb-prose">{@html tr('onboarding.consentLabel')}</Text>
        </div>
        <div class="consent-hint" class:shown={consentBlocked}>
          <Text size="xs" tone="muted" id="wlc-consent-hint">{tr('onboarding.consentHint')}</Text>
        </div>
      {:else if current.kind === 'choice'}
        <div class="role-choices">
          <RadioGroup
            variant="cards"
            cols={compactChoices.current ? 2 : 4}
            terse={terseChoices.current}
            name="setup"
            label={tr('onboarding.choiceTitle')}
            value={setup}
            options={choiceOptions}
            onValueChange={pickSetup}
            aria-describedby="wlc-preview-{setup}"
            onpointerover={(e: PointerEvent) => previewChoice(e.target)}
            onfocusin={(e: FocusEvent) => previewChoice(e.target)}
            onpointerleave={clearChoice}
            onfocusout={clearChoice}
          >
            {#snippet leading(option, on)}
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
        <LanguageSwitcher label={tr('lang.switchAria')} options={langOptions} variant="field" onSelect={(code) => setLang(code as Locale)} />
        <div class="bb-setup-row">
          <div class="pref-text">
            <Text as="span" size="sm"><strong>{tr('settings.customCursor')}</strong></Text>
            <Text size="xs" tone="muted" id="wlc-cursor-hint">{tr('settings.customCursorHint')}</Text>
          </div>
          <CursorSwitch describedby="wlc-cursor-hint" />
        </div>
      {:else if current.kind === 'mod'}
        <div class="mod-well" use:decode>
          <CopySurface
            variant="well"
            text={MOD_COMMAND}
            hint={tr('common.copy')}
            copiedLabel={tr('common.copied')}
            flashMs={2000}
            legacyFallback
            title={tr('common.copy')}
            onCopy={celebrateCopy}
          >
            {#snippet children()}<span data-decode={MOD_COMMAND}>{MOD_COMMAND}</span>{/snippet}
          </CopySurface>
        </div>
      {:else if current.kind === 'commands'}
        <ChatPreview name="hello" response={tr('onboarding.exampleReply')} tag={tr('onboarding.exampleLabel')} broadcasterName={data.name} {locale} samples={{ user: tr('onboarding.exampleViewer') }} />
        <Text size="xs" tone="muted">{tr('onboarding.newCommandsHint')}</Text>
      {:else if current.kind === 'modules'}
        <ChatPreview kind="reply" name="welcome" viewerText={tr('onboarding.moduleExampleViewer')} response={tr('onboarding.moduleExampleReply')} tag={tr('onboarding.moduleExampleLabel')} broadcasterName={data.name} {locale} samples={{ user: tr('onboarding.exampleViewer') }} />
        <Text size="xs" tone="muted">{tr('onboarding.newModulesHint')}</Text>
      {/if}

      <div
        class="bb-setup-actions"
        role="group"
        data-arrows-ok
        onpointerover={(e) => (hover = reactionFor(e.target))}
        onpointerout={() => (hover = null)}
      >
        {#if current.kind === 'hero'}
          <Button data-react="excited" onclick={goNext}>{tr('onboarding.heroCta')}</Button>
        {:else}
          <Button variant="ghost" onclick={goBack}>{tr('onboarding.back')}</Button>
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
        {/if}
      </div>
      {#snippet status()}
        {#if saveError}<AlertBanner tone="danger">{saveErrorServer || tr('onboarding.saveError')}</AlertBanner>{/if}
      {/snippet}
    </SetupStep>
  {/key}
</SetupStage>

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
  .consent { display: flex; align-items: center; gap: 14px; }
  .consent-hint { min-height: 20px; visibility: hidden; opacity: 0; transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo); }
  .consent-hint.shown { visibility: visible; opacity: 1; }
  .mod-well { display: flex; justify-content: center; }
  .pref-text { display: grid; gap: 4px; flex: 1; min-width: 0; }

  .role-choices { --choice-min-h: 132px; width: 100%; }
  .role-previews { display: grid; margin-top: 12px; text-align: center; }
  .role-preview {
    grid-area: 1 / 1;
    max-width: 62ch;
    justify-self: center;
    visibility: hidden;
    opacity: 0;
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo), visibility 0s linear var(--bb-dur-base);
  }
  .role-preview.shown { visibility: visible; opacity: 1; transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo); }
  .role-glyph {
    display: inline-grid;
    place-items: center;
    flex: none;
    width: 32px;
    height: 32px;
    border-radius: var(--bb-radius-sm);
    background: var(--bb-elev-2);
    color: var(--bb-muted);
    transition: color var(--bb-dur-fast) ease;
  }
  .role-glyph.on { color: var(--bb-green-glow); }

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


  @media (prefers-reduced-motion: reduce) {
    .role-preview, .consent-hint, .role-glyph { transition: none; }
    .veil { animation: none; }
  }
</style>
