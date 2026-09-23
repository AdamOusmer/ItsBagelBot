<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // A first-visit journey: welcome, agreement, mod guidance and a choice of
  // starting setup. New streamers receive a longer introduction; the other
  // paths apply their setup and continue to the dashboard or import wizard.
  //
  // The motion is one lateral primitive used everywhere, on the site's one
  // curve. The blob and the copy swap sides on every step, so a step change
  // is two halves of the screen crossing each other: the blob dashes across
  // while the old copy leaves toward the direction of travel and the new copy
  // arrives from the far side, line by line, each line from the side opposite
  // the one before it. The sky (Sky.svelte) leans the other way underneath.
  // Nothing bounces, nothing pops in; everything slides and settles.
  //
  // The page owns its own translator (`tr`, below) instead of the app's i18n
  // context, because the language step must not reload: the app's LangSwitch
  // posts a real form to /lang and comes back through a full load, which
  // restarted the whole tour (blob entrance, settle, back to step one) to
  // change two words. Here the catalog swaps live and the server is told in
  // the background. The accepted consent sits in sessionStorage so a reload
  // does not ask twice within one tab.
  import { onMount, tick } from 'svelte';
  import { fly } from 'svelte/transition';
  import { page } from '$app/state';
  import { goto, onNavigate, pushState, replaceState } from '$app/navigation';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { copyText } from '@bagel/ui/lib/clipboard';
  import { bezier } from '@bagel/ui/lib/tween';
  import { hasFinePointer, prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import { decode } from '@bagel/ui/svelte/actions';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import type { IconName } from '@bagel/ui/lib/icons';
  import Toggle from '@bagel/ui/svelte/Toggle.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { LOCALES, ensureCatalog, translate, type Locale } from '@bagel/kit/i18n';
  // Side-effect import: the language control below writes the
  // `.bb-lang-switch` contract's classes itself (it is a row of buttons, not
  // the app's form), so nothing else pulls the stylesheet in on its behalf.
  import '@bagel/ui/styles/elements/nav.css';
  import CursorSwitch from '$lib/components/CursorSwitch.svelte';
  import Sky from '$lib/components/welcome/Sky.svelte';
  import StepRail from '$lib/components/welcome/StepRail.svelte';
  import Completion from '$lib/components/welcome/Completion.svelte';
  import ImportWizard from '$lib/components/welcome/ImportWizard.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';

  let { data } = $props();

  // The language this page is speaking. Starts as the app's, then follows the
  // language step live; every string below reads it through `tr`, so a switch
  // re-renders the copy in place.
  let locale = $state<Locale>(getI18n().locale);
  const tr = (key: string, params?: Record<string, string | number>) => translate(locale, key, params);

  type Kind = 'hero' | 'consent' | 'mod' | 'choice' | 'lang' | 'commands' | 'modules';
  type Setup = 'start-new' | 'integrate' | 'quiet' | 'import';
  type Beat = {
    kind: Kind;
    /** The blob's resting face while this step is up. Positive only. */
    face: string;
    title: string;
    body: string;
  };

  const MOD_COMMAND = '/mod ItsBagelBot';

  // The steps of the tour proper, by message key. Translated below, in
  // whatever language the page is speaking at the time.
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

  // One row per starting setup: its message-key stem, its glyph, and the face
  // Bolota pulls while the pointer weighs it, so the blob previews the mood
  // of each path (eager to teach, plugged in alongside, winding down, ready
  // to carry things across) before anything is picked.
  const CHOICES = [
    { id: 'start-new', key: 'New', icon: 'start', face: 'excited' },
    { id: 'integrate', key: 'Integrate', icon: 'integrate', face: 'attentive' },
    { id: 'quiet', key: 'Quiet', icon: 'quiet', face: 'sleepy' },
    { id: 'import', key: 'Import', icon: 'importFile', face: 'curious' }
  ] as const satisfies readonly { id: Setup; key: string; icon: IconName; face: string }[];
  const choiceOf = (id: Setup) => CHOICES.find((c) => c.id === id) ?? CHOICES[0];
  const choiceIndex = $derived(Math.max(0, CHOICES.findIndex((c) => c.id === (hoveredChoice ?? setup))));
  const choicePosition = $derived(['12.5%', '37.5%', '62.5%', '87.5%'][choiceIndex]);
  function pickSetup(id: Setup) {
    if (setup === id) return;
    setup = id;
    react(choiceOf(id).face, 1400);
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
  const railLabels = $derived([
    ...tour.map((s) => s.title),
    ...(setup === 'import' ? importStageKeys.map((key) => tr(key)) : [])
  ]);
  const journeyTotal = $derived(railLabels.length);
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
  // pushState leaves page.url on the old URL, so the wizard opened from the
  // tour is read from page.state; the ?import=1 link covers an OAuth return.
  let importLink = $state(importRequested);
  const importActive = $derived(page.state.importing ?? importLink);
  let step = $state(importRequested ? choiceStep : 0);
  let furthest = $state(importRequested ? choiceStep : 0);
  // Direction of travel: +1 forward, -1 back. Decides which side the new copy
  // arrives from and which side the old copy leaves toward.
  let dir = $state(1);
  let consentAccepted = $state(false);
  let leaving = $state(false);
  let showCompletion = $state(false);
  let saveError = $state(false);
  let copied = $state(false);

  const current = $derived(steps[step]);
  const last = $derived(step === steps.length - 1);
  const consentBlocked = $derived(!consentAccepted);
  /** Furthest step reachable right now. */
  const maxStep = $derived(consentBlocked ? consentStep : Math.min(furthest + 1, steps.length - 1));
  const nextDisabled = $derived(step + 1 > maxStep);
  /** Whether the arrow key may move forward: not gated, and not on the last step. */
  const canAdvance = $derived(!nextDisabled && !last);
  /** The blob's side of the stage; the copy takes the other one. */
  const blobRight = $derived(step % 2 === 1);
  const progress = $derived(
    setup === 'import'
      ? Math.max(step - 1, 0) / Math.max(journeyTotal - 1, 1)
      : step / Math.max(steps.length - 1, 1)
  );

  // ── Motion ───────────────────────────────────────────────────────────
  // The site's one curve (--bb-ease-out-expo), solved in JS for the Svelte
  // transitions below so they land on exactly the same deceleration the CSS
  // transitions on the blob, the sky and the rail use.
  const expo = bezier(0.16, 1, 0.3, 1);
  const TRAVEL = 72;
  // Line `i` arrives from the direction of travel when even and from the
  // opposite side when odd, so a step's four lines close on the column from
  // both sides instead of marching in from one.
  const sideOf = (i: number) => (i % 2 === 0 ? 1 : -1);
  const arrive = (node: Element, { i = 0 }: { i?: number } = {}) =>
    prefersReducedMotion()
      ? { duration: 0 }
      : fly(node, { x: dir * TRAVEL * sideOf(i), duration: 760, delay: 160 + i * 70, easing: expo, opacity: 0 });
  const depart = (node: Element, { i = 0 }: { i?: number } = {}) =>
    prefersReducedMotion() || departed
      ? { duration: 0 }
      : fly(node, { x: -dir * 56 * sideOf(i), duration: 380, delay: i * 35, easing: expo, opacity: 0 });

  // The choice step lays the stage out differently (blob above a full-width
  // row of cards), and a grid change cannot transition. Swapping it under a
  // departing step snapped that step's copy into the new layout mid-exit, so
  // it dropped and slid under the arriving cards. Moves that change layout
  // clear the stage first, relayout while it is empty, then arrive.
  const CLEAR_MS = 300;
  let clearing = $state(false);
  let departed = false;
  const isChoice = (i: number) => steps[i]?.kind === 'choice';

  // The blob's one-shot for each kind of move. A bare state swap snaps; a
  // sequence carries the blob across the change. `entrance` plays once on
  // mount (Bolota fires the initial sequence when its engine is ready).
  type Sequence = 'entrance' | 'burst' | 'orbit' | 'comet';
  let sequence = $state<Sequence>('entrance');
  let sequenceKey = $state(0);
  function play(seq: Sequence) {
    sequence = seq;
    sequenceKey += 1;
  }

  // Held faces outrank the step's resting face: a timed reaction (consent
  // ticked, command copied) first, then whatever the pointer is over.
  let reaction = $state<string | null>(null);
  let hover = $state<string | null>(null);
  let reactionTimer: ReturnType<typeof setTimeout> | null = null;
  const expression = $derived(reaction ?? hover ?? current.face);
  function react(face: string, ms: number) {
    reaction = face;
    if (reactionTimer) clearTimeout(reactionTimer);
    reactionTimer = setTimeout(() => (reaction = null), ms);
  }
  // Pleased on the control that moves the tour forward, merely interested in
  // anything else it can click. Same rule the coach-mark version had.
  function reactionFor(target: EventTarget | null): string | null {
    const el = target instanceof Element ? target.closest('button, a') : null;
    if (!el || el.hasAttribute('disabled')) return null;
    return el.matches('.bb-btn--primary, .bb-btn--solid') ? 'excited' : 'curious';
  }

  // Pointer parallax for the sky, -1..1 from the viewport centre. Coalesced
  // to one write per frame; a 120Hz pointer stream would otherwise set the
  // style attribute twice per painted frame for nothing.
  let px = $state(0);
  let py = $state(0);
  let pointerFrame = 0;
  function onPointerMove(e: PointerEvent) {
    if (!hasFinePointer() || pointerFrame) return;
    pointerFrame = requestAnimationFrame(() => {
      pointerFrame = 0;
      px = (e.clientX / window.innerWidth - 0.5) * 2;
      py = (e.clientY / window.innerHeight - 0.5) * 2;
    });
  }

  // ── Navigation ───────────────────────────────────────────────────────
  function go(i: number) {
    // Clamped rather than trusted: the controls are disabled past `maxStep`
    // and this is the second lock, so a keyboard activation or a stale render
    // cannot land on a step the gate has not opened yet.
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
    // Swirl keeps Bolota's base face active, so pointer gaze remains live.
    play('entrance');
  }
  const goNext = () => go(step + 1);
  const goBack = () => go(step - 1);
  // From the index: a forward jump is the same dash as Next, a backward one
  // the same turn as Back. The rail counts the tour only, so +1 for the hello.
  const goTo = (i: number) => go(i + 1);

  const CONSENT_KEY = 'bb-welcome-consent';
  onMount(() => {
    try {
      consentAccepted = sessionStorage.getItem(CONSENT_KEY) === '1';
      if (importRequested && !consentAccepted) {
        importLink = false;
        replaceState('/welcome', {});
        step = consentStep;
      }
    } catch {
      /* storage blocked: the box simply starts unticked */
    }
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

  // In place, no navigation (see the header comment). The account and the
  // cookie still get the choice, so every page after this one loads in the
  // new language; the page itself is already speaking it by the time the
  // request leaves. Fire and forget: a failed save shows up as the next page
  // loading in the old language, which the settings page fixes in one click.
  async function setLang(l: Locale) {
    if (l === locale) return;
    await ensureCatalog(l);
    locale = l;
    document.documentElement.lang = l;
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
      /* storage blocked: a reload asks again, which is the safe direction */
    }
    if (on) react('excited', 1600);
  }

  async function copyMod() {
    const ok = await copyText(MOD_COMMAND, { legacyFallback: true });
    if (!ok) return;
    copied = true;
    react('proud', 2200);
    play('entrance');
    setTimeout(() => (copied = false), 2000);
  }

  // ── Exit ─────────────────────────────────────────────────────────────
  // Apply the chosen setup before playing the completion beat.
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
  // The shell's translator is fixed at boot, so a tour that switched language
  // must leave with a full load; the veil fades to the shell's ground first so
  // the load lands on the colour it paints. Otherwise the router takes over and
  // the view transition below melts the sky into the dashboard.
  const bootLocale = getI18n().locale;
  const VEIL_MS = 420;
  let veiled = $state(false);
  function exitTo(to: string) {
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
      destination = result.location;
      showCompletion = true;
      return;
    }
    leaving = false;
    saveError = true;
    react('attentive', 2400);
  };

  function onKeydown(e: KeyboardEvent) {
    if (leaving || importActive) return;
    if (e.key === 'ArrowRight' && canAdvance) {
      e.preventDefault();
      goNext();
    } else if (e.key === 'ArrowLeft' && step > 0) {
      e.preventDefault();
      goBack();
    }
  }

  // Screen readers land on the new step's heading rather than wherever focus
  // was. Not on the first render: stealing focus on page load is the thing
  // browsers deliberately do not do.
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
  <!-- Signed-in surface: never indexed. -->
  <meta name="robots" content="noindex, nofollow" />
</svelte:head>

<svelte:window onkeydown={onKeydown} onpointermove={onPointerMove} />

{#if importActive}
  <ImportWizard onback={returnFromImport} onexit={exitTo} {consentAccepted} {locale} />
{:else}
<Sky shift={blobRight ? 1 : -1} turn={step * 24} {px} {py} {progress} {leaving} />

<div class="welcome" class:leaving data-welcome>
  <header class="top">
    <Brand title="ItsBagelBot" sub={tr('common.console')} logoSrc="/logo.png" logoAlt="" size="md" />
    <StepRail
      labels={railLabels}
      current={step - 1}
      maxStep={maxStep - 1}
      label={tr('onboarding.stepOf', { n: Math.max(step, 1), total: journeyTotal })}
      onselect={goTo}
    />
  </header>

  <main class="stage">
    <div class="pair" class:choice-stage={current.kind === 'choice'} class:clearing style="--choice-position: {choicePosition}; --dir: {dir};">
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
            <p class="kicker" in:arrive={{ i: 0 }} out:depart={{ i: 0 }}>
              {step === 0 ? tr('onboarding.title') : tr('onboarding.stepOf', { n: step, total: journeyTotal })}
            </p>
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
            <p class="body" in:arrive={{ i: 2 }} out:depart={{ i: 2 }}>{current.body}</p>

            {#if current.kind === 'consent'}
              <div class="control consent" in:arrive|global={{ i: 3 }} out:depart|global={{ i: 3 }}>
                <Toggle bind:on={consentAccepted} onchange={onConsent} />
                <!-- `.bb-prose`: a TRANSLATED string with two links inside it,
                     so this file cannot put a class on either anchor; the
                     typography contract styles them. -->
                <span class="bb-prose">{@html tr('onboarding.consentLabel')}</span>
              </div>
            {:else if current.kind === 'choice'}
              <div
                class="control role-choices"
                role="group"
                aria-label={tr('onboarding.choiceTitle')}
                in:arrive|global={{ i: 3 }}
                out:depart|global={{ i: 3 }}
                onpointerleave={() => { hover = null; hoveredChoice = null; }}
              >
                {#each CHOICES as choice, ci (choice.id)}
                  <button
                    type="button"
                    class="role-choice"
                    class:selected={setup === choice.id}
                    aria-pressed={setup === choice.id}
                    style="--side: {ci % 2 ? 1 : -1};"
                    onclick={() => pickSetup(choice.id)}
                    onpointerenter={() => { hover = choice.face; hoveredChoice = choice.id; }}
                    onfocus={() => { hover = choice.face; hoveredChoice = choice.id; }}
                    onblur={() => { hover = null; hoveredChoice = null; }}
                  >
                    <span class="role-top">
                      <span class="role-glyph" aria-hidden="true"><Icon name={choice.icon} size={15} /></span>
                      <span class="role-title">{tr(`onboarding.choice${choice.key}Title`)}</span>
                      <span class="role-indicator" aria-hidden="true"><Icon name="check" size={12} /></span>
                    </span>
                    <span class="role-detail">{tr(`onboarding.choice${choice.key}Body`)}</span>
                  </button>
                {/each}
              </div>
            {:else if current.kind === 'lang'}
              <div class="control prefs" in:arrive|global={{ i: 3 }} out:depart|global={{ i: 3 }}>
                <div class="lang-row">
                  <div class="bb-lang-switch" role="group" aria-label={tr('lang.switchAria')}>
                    {#each LOCALES as l (l)}
                      <button
                        type="button"
                        class="bb-lang-switch__opt"
                        class:is-active={l === locale}
                        aria-pressed={l === locale}
                        onclick={() => setLang(l)}
                      >{l.toUpperCase()}</button>
                    {/each}
                  </div>
                </div>
                <div class="pref-row">
                  <div>
                    <span class="pref-name">{tr('settings.customCursor')}</span>
                    <p class="pref-hint" id="wlc-cursor-hint">{tr('settings.customCursorHint')}</p>
                  </div>
                  <CursorSwitch describedby="wlc-cursor-hint" />
                </div>
              </div>
            {:else if current.kind === 'mod'}
              <!-- The command decodes into place (the brand's decrypt reveal)
                   and the whole well is the copy button, so there is nothing
                   smaller to aim for. -->
              <button
                type="button"
                class="control well"
                class:copied
                onclick={copyMod}
                title={tr('common.copy')}
                use:decode
                in:arrive|global={{ i: 3 }}
                out:depart|global={{ i: 3 }}
              >
                <code class="cmd" data-decode={MOD_COMMAND}>{MOD_COMMAND}</code>
                <span class="hint">
                  <Icon name={copied ? 'check' : 'copy'} size={12} />
                  {copied ? tr('common.copied') : tr('common.copy')}
                </span>
              </button>
            {:else if current.kind === 'commands'}
              <div class="control rehearsal-example" in:arrive|global={{ i: 3 }} out:depart|global={{ i: 3 }}>
                <ChatPreview name="hello" response={tr('onboarding.exampleReply')} tag={tr('onboarding.exampleLabel')} broadcasterName={data.name} {locale} samples={{ user: tr('onboarding.exampleViewer') }} />
                <p class="example-note">{tr('onboarding.newCommandsHint')}</p>
              </div>
            {:else if current.kind === 'modules'}
              <div class="control rehearsal-example" in:arrive|global={{ i: 3 }} out:depart|global={{ i: 3 }}>
                <ChatPreview kind="reply" name="welcome" viewerText={tr('onboarding.moduleExampleViewer')} response={tr('onboarding.moduleExampleReply')} tag={tr('onboarding.moduleExampleLabel')} broadcasterName={data.name} {locale} samples={{ user: tr('onboarding.exampleViewer') }} />
                <p class="example-note">{tr('onboarding.newModulesHint')}</p>
              </div>
            {/if}

            <div
              class="actions"
              role="group"
              in:arrive={{ i: 4 }}
              out:depart={{ i: 4 }}
              onpointerover={(e) => (hover = reactionFor(e.target))}
              onpointerout={() => (hover = null)}
            >
              {#if current.kind === 'hero'}
                <button type="button" class="bb-btn bb-btn--primary" onclick={goNext}>
                  {tr('onboarding.heroCta')}
                </button>
              {:else}
                {#if last}
                  {#if setup === 'import'}
                    <button type="button" class="bb-btn bb-btn--primary" onclick={startImport}>{tr('onboarding.choiceImportCta')}</button>
                  {:else}
                    <button type="button" class="bb-btn bb-btn--primary" onclick={finish}>{tr('onboarding.finishCta')}</button>
                  {/if}
                {:else}
                  <button type="button" class="bb-btn bb-btn--primary" onclick={goNext} disabled={nextDisabled}>
                    {tr('onboarding.next')}
                  </button>
                {/if}
                <button type="button" class="quiet" onclick={goBack}>{tr('onboarding.back')}</button>
              {/if}
            </div>
            {#if saveError}
              <p class="error" role="alert">{tr('onboarding.saveError')}</p>
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
    <div class="meta">
      <span class="count">{step === 0 ? '' : tr('onboarding.stepOf', { n: step, total: journeyTotal })}</span>
    </div>
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
  /* The shell paints its ambient orb pair on every route (RootShell ->
     BackgroundOrbs); this page paints its own sky and the pair would muddy
     it. Gated on this page's presence so the rule unscopes itself the moment
     the page unmounts, and it holds during SSR too, so the shell orbs never
     flash in before hydration. Same shape as /login. */
  :global(body:has([data-welcome]) .bb-bg-orb) { display: none; }

  .welcome {
    --gutter: clamp(20px, 4vw, 48px);
    --gap: clamp(24px, 5vw, 72px);
    position: relative;
    z-index: 1;
    /* svh, not dvh: the small viewport height does not change as a phone's
       address bar collapses, so the footer never rides up and down. */
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
    /* `backwards`, not `both`: a held final keyframe pins opacity at 1, and
       the exit fade (.leaving .top) could never apply. */
    animation: settle 900ms var(--bb-ease-out-expo) backwards;
  }

  .stage {
    display: grid;
    place-items: center;
    padding: 24px var(--gutter);
  }

  /* A fixed-height row, with the copy anchored to its top and the blob
     centred in it. Centring the copy instead (the obvious layout) re-centred
     it on every step, because each step's control is a different height, so
     the title rode up and down as the tour went on. Now it stays put and the
     content grows downward, and the row is tall enough that no step's copy
     makes it grow. */
  .pair {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: var(--gap);
    align-items: start;
    width: min(100%, 1120px);
    min-height: clamp(440px, 64svh, 720px);
    transition:
      transform 640ms var(--bb-ease-out-expo),
      opacity 520ms var(--bb-ease-out-expo);
  }

  /* The swap. Both columns are the same width, so translating each by its
     own width plus the gap trades their places exactly, and because it is a
     transform it can be a 1s glide instead of a reflow. */
  .blob-col,
  .copy-col {
    transition:
      transform 1000ms var(--bb-ease-out-expo),
      opacity 480ms var(--bb-ease-out-expo);
    will-change: transform;
  }
  .blob-col.right { transform: translateX(calc(100% + var(--gap))); }
  .copy-col.left { transform: translateX(calc(-100% - var(--gap))); }

  .step {
    transition:
      transform 300ms var(--bb-ease-out-expo),
      opacity 240ms var(--bb-ease-out-expo);
  }
  .clearing .blob-col { opacity: 0; transition-duration: 1000ms, 240ms; }
  .clearing .step {
    opacity: 0;
    transform: translateX(calc(var(--dir) * -56px));
  }

  /* ── Blob ─────────────────────────────────────────────────────────── */
  .blob-col {
    --blob-scale: 0.86;
    display: flex;
    justify-content: center;
    align-self: center;
  }
  .blob-col.hero { --blob-scale: 1.1; }

  /* Three nested transforms because they must not share a property: the
     scale transitions between steps, the float is a keyframe loop, and the
     engine draws inside. One element could not do the first two at once. */
  .scale {
    transform: scale(var(--blob-scale));
    transition: transform 1000ms var(--bb-ease-out-expo);
  }

  .float {
    position: relative;
    filter: drop-shadow(0 18px 34px rgba(0, 0, 0, 0.42));
    animation: float 9s ease-in-out infinite;
  }

  /* The plate behind the blob: a breathing glow in the two brand colours,
     under the shadow so the blob reads as lit rather than as pasted on. */
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

  /* ── Copy ─────────────────────────────────────────────────────────── */
  /* Grid-stacked so the leaving step and the arriving one overlap during
     the handover instead of stacking vertically for a frame. */
  .copy-col {
    display: grid;
    max-width: 560px;
    justify-self: start;
    padding-top: clamp(16px, 7svh, 72px);
  }
  .copy-col > .step { grid-area: 1 / 1; }

  /* The four setups share one line. Bolota moves above the card being
     considered and keeps its pointer gaze while the selected face changes. */
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
    transition: left 680ms var(--bb-ease-out-expo), transform 1000ms var(--bb-ease-out-expo);
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

  .kicker {
    margin: 0 0 14px;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-tan);
  }

  /* Display type with a specular sweep: the gradient is mostly the text
     colour with a pale band in the middle, and the band travels across once
     on arrival. Re-keyed per step, so every title gets its own pass. */
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

  .body {
    margin: 0;
    max-width: 46ch;
    font-family: var(--bb-font-body);
    font-size: clamp(1rem, 1.35vw, 1.125rem);
    line-height: 1.6;
    color: var(--bb-muted);
  }

  .control { margin-top: 24px; }

  .role-choices { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; width: 100%; }
  .role-choice {
    position: relative;
    display: grid;
    align-content: start;
    gap: 8px;
    width: 100%;
    min-height: 132px;
    padding: 13px;
    overflow: hidden;
    text-align: left;
    background: rgba(255, 255, 255, 0.03);
    color: var(--bb-white);
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    cursor: pointer;
    transition:
      border-color 240ms ease,
      background 240ms ease,
      box-shadow 420ms var(--bb-ease-out-expo);
  }
  /* The selection light sweeps in from the card's own side (left column
     from the left, right column from the right), the same crossing motion
     the stage uses. A transform on a pseudo-element, so no reflow. */
  .role-choice::before {
    content: '';
    position: absolute;
    inset: 0;
    z-index: -1;
    background: linear-gradient(100deg, rgba(var(--bb-tan-rgb), 0.16), rgba(var(--bb-green-glow-rgb), 0.08));
    transform: translateX(calc(var(--side) * 101%));
    transition: transform 620ms var(--bb-ease-out-expo);
  }
  .role-choice { isolation: isolate; }
  .role-choice.selected::before { transform: none; }
  .role-choice:hover, .role-choice.selected { border-color: var(--bb-tan); }
  .role-choice.selected { box-shadow: 0 0 0 1px rgba(var(--bb-tan-rgb), 0.35), 0 14px 32px rgba(0, 0, 0, 0.22); }
  .role-choice:focus-visible { outline: 2px solid var(--bb-tan); outline-offset: 2px; }
  .role-top { display: grid; grid-template-columns: 1fr auto; align-items: start; gap: 8px; }
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
      transform 520ms var(--bb-ease-out-expo),
      color 240ms ease,
      border-color 240ms ease;
  }
  .role-choice:hover .role-glyph { transform: translateX(3px); }
  .role-choice.selected .role-glyph {
    color: var(--bb-green-glow);
    border-color: rgba(var(--bb-green-glow-rgb), 0.5);
  }
  .role-title { grid-column: 1 / -1; min-width: 0; font: 700 13px/1.2 var(--bb-font-display); }
  /* The tick is always there and only scales, so picking a card never
     shifts its title. */
  .role-indicator {
    display: inline-grid;
    place-items: center;
    flex: none;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    border: 1px solid var(--bb-border-strong);
    color: var(--bb-bg-0, #101611);
    transition:
      background 240ms ease,
      border-color 240ms ease;
  }
  .role-indicator :global(svg) {
    transform: scale(0);
    transition: transform 420ms var(--bb-ease-out-expo);
  }
  .role-choice.selected .role-indicator {
    background: var(--bb-green-glow);
    border-color: var(--bb-green-glow);
  }
  .role-choice.selected .role-indicator :global(svg) { transform: scale(1); }
  .role-detail { font: 11px/1.35 var(--bb-font-body); color: var(--bb-muted); }
  .rehearsal-example { width: min(100%, 480px); text-align: left; }
  .example-note { margin: 12px 0 0; color: var(--bb-muted); font: 12px/1.5 var(--bb-font-body); }

  .consent {
    display: flex;
    align-items: center;
    gap: 14px;
    font-family: var(--bb-font-body);
    font-size: 14px;
    color: var(--bb-muted);
  }

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
  .pref-name {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 14px;
    color: var(--bb-white);
  }
  .pref-hint {
    margin: 4px 0 0;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--bb-muted);
  }

  /* Premium surface, not an accent-bordered box: gradient fill, hairline,
     top-edge inset highlight, deep shadow. Hover slides it, it does not
     grow. */
  .well {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 14px;
    width: min(100%, 440px);
    padding: 15px 16px;
    background: linear-gradient(180deg, rgba(255, 255, 255, 0.04), rgba(0, 0, 0, 0.32));
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-sm);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.07),
      0 18px 40px rgba(0, 0, 0, 0.35);
    cursor: pointer;
    transition:
      border-color var(--bb-dur-base) var(--bb-ease-out-expo),
      transform var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .well:hover {
    border-color: var(--bb-tan);
    transform: translateX(6px);
  }
  .cmd {
    font-family: var(--bb-font-mono);
    font-size: 15px;
    letter-spacing: 0.02em;
    color: var(--bb-tan-pale);
  }
  .hint {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 12px;
    color: var(--bb-muted);
    transition: color var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .well:hover .hint { color: var(--bb-tan-pale); }
  .well.copied { border-color: var(--bb-green-glow); }
  .well.copied .hint { color: var(--bb-green-glow); }
  .well:focus-visible { outline: 2px solid var(--bb-tan); outline-offset: 2px; }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px 14px;
    margin-top: 28px;
  }

  .quiet {
    padding: 8px 4px;
    border: 0;
    background: none;
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    color: var(--bb-muted);
    cursor: pointer;
    transition:
      color var(--bb-dur-base) var(--bb-ease-out-expo),
      transform var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .quiet:hover {
    color: var(--bb-tan-pale);
    transform: translateX(3px);
  }

  .error {
    margin: 12px 0 0;
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-status-error, #e5484d);
  }

  /* ── Foot ─────────────────────────────────────────────────────────── */
  .foot {
    display: grid;
    gap: 10px;
    padding: 0 var(--gutter) 18px;
    animation: settle 900ms var(--bb-ease-out-expo) 120ms backwards;
  }
  .meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    /* The row's height is this, not its contents': the counter is empty on
       the hello and the skip fades out on the last step. */
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
    transition: transform 900ms var(--bb-ease-out-expo);
  }
  /* The head of the progress line: a lit bead riding the fill's leading
     edge. Its track is the line's own width, so translating it by --p of
     100% lands it exactly where scaleX(--p) ends. */
  .bead-track {
    position: absolute;
    inset: 0;
    transform: translateX(calc(var(--p) * 100%));
    transition: transform 900ms var(--bb-ease-out-expo);
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
  .count {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  /* ── Exit ─────────────────────────────────────────────────────────── */
  .leaving .pair {
    transform: translateX(-6vw);
    opacity: 0;
  }
  .leaving .top,
  .leaving .foot {
    opacity: 0;
    transition: opacity 400ms var(--bb-ease-out-expo);
  }
  .veil {
    position: fixed;
    inset: 0;
    z-index: 200;
    background: var(--bb-bg-0, #101611);
    animation: veil-in 420ms var(--bb-ease-out-expo) both;
  }
  @keyframes veil-in { from { opacity: 0; } }
  :global(::view-transition-old(root)),
  :global(::view-transition-new(root)) {
    animation-duration: 720ms;
    animation-timing-function: cubic-bezier(0.16, 1, 0.3, 1);
  }

  @keyframes settle {
    from { opacity: 0; transform: translateX(-16px); }
    to { opacity: 1; transform: none; }
  }
  /* Lateral by design: the idle drift travels sideways more than it rises,
     so the resting blob reads as hovering, not as bobbing on a string. */
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
  /* Light: heavy black elevation reads as a smudge on paper; ink alphas keep
     the lift. */
  :global(:root[data-theme="light"]) .float { filter: drop-shadow(0 14px 28px rgba(20, 17, 12, 0.16)); }
  :global(:root[data-theme="light"]) .well {
    background: linear-gradient(180deg, rgba(255, 255, 255, 0.5), rgba(20, 17, 12, 0.05));
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.6),
      0 14px 30px rgba(20, 17, 12, 0.12);
  }

  /* Phones cannot fit a 240px blob beside a column of copy: stack them,
     centre everything, and drop the side swap, since there are no sides. */
  @media (max-width: 760px) {
    .pair {
      grid-template-columns: 1fr;
      gap: 8px;
      justify-items: center;
      text-align: center;
    }
    .blob-col.right,
    .copy-col.left { transform: none; }
    /* Anchored to the top rather than centred, and the blob slot is one
       height for every step: the same rule as the desktop row, nothing on
       the page moves when the copy under it changes length. */
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
    .well { margin-inline: auto; }
    .role-choices, .rehearsal-example { margin-inline: auto; }
    .role-choices { text-align: left; }
  }

  @media (max-width: 560px) {
    .role-choices {
      grid-template-columns: repeat(4, minmax(150px, 1fr));
      overflow-x: auto;
      scroll-snap-type: x mandatory;
      padding-bottom: 8px;
    }
    .role-choice { scroll-snap-align: start; }
  }

  @media (prefers-reduced-motion: reduce) {
    .pair,
    .blob-col,
    .copy-col,
    .step,
    .scale,
    .fill,
    .well,
    .role-choice,
    .role-choice::before,
    .role-glyph,
    .role-indicator :global(svg),
    .choice-stage .blob-col .scale,
    .bead-track,
    .quiet { transition: none; }
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
