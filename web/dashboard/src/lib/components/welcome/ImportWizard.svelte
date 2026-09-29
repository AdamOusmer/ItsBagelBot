<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount, tick, untrack } from 'svelte';
  import { fly } from 'svelte/transition';
  import { page } from '$app/state';
  import { translate, translateList, type Locale } from '@bagel/kit/i18n';
  import { bezier } from '@bagel/ui/lib/tween';
  import { hasFinePointer, prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import Sky from '@bagel/ui/svelte/Sky.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import StepRail from '$lib/components/welcome/StepRail.svelte';
  import Completion from '$lib/components/welcome/Completion.svelte';
  import ImportAlert from '$lib/import/ImportAlert.svelte';
  import ImportSkipped from '$lib/import/ImportSkipped.svelte';
  import { ImportSession } from '$lib/import/session.svelte';
  import {
    clearSnapshot,
    loadSnapshot,
    retainedSnapshot,
    retainSnapshot,
    saveSelection,
    saveSnapshot
  } from '$lib/import/persist';
  import { deserialize } from '$app/forms';
  import {
    AlertBanner,
    PermBadge,
    Bolota,
    Button,
    ButtonLink,
    Card,
    Checkbox,
    ConfirmDialog,
    Heading,
    Icon,
    Tag,
    Text,
    Textarea
  } from '@bagel/kit';
  import {
    CHIP_LABEL_KEYS,
    IMPORT_STRATEGIES,
    isImportSource,
    type ImportSourceStrategy,
    type InputSpec
  } from '@bagel/kit/importer/strategy';
  import { IMPORT_SOURCES, type ImportSource } from '@bagel/kit';

  let { onback, onexit, consentAccepted, locale }: { onback: (welcomeStep: number) => void; onexit: (to: string) => void; consentAccepted: boolean; locale: Locale } = $props();
  const t = (key: string, params?: Record<string, string | number>) => translate(locale, key, params);
  const tl = (key: string) => translateList(locale, key);

  const BEFORE_IMPORT = $derived([
    t('onboarding.consentTitle'),
    t('onboarding.step1Title'),
    t('onboarding.langTitle'),
    t('onboarding.choiceTitle')
  ]);
  const STAGES = $derived([
    t('onboardingImport.stagePick'),
    t('onboardingImport.stageConnect'),
    t('onboardingImport.stageCommands'),
    t('onboardingImport.stageExtras'),
    t('onboardingImport.stageReview'),
    t('onboardingImport.stageDone')
  ]);

  type Step = 'pick' | 'instructions' | 'commands' | 'extras' | 'review' | 'done';
  const ORDER: Step[] = ['pick', 'instructions', 'commands', 'extras', 'review', 'done'];

  const session = new ImportSession();
  session.configure({ t, isConnected: () => sourceConnected, onPosting: () => play('entrance') });
  const resumed = retainedSnapshot();
  if (resumed) session.restore(resumed);
  applyDeepLink();

  const step = $derived(session.stage as Step);

  function deepLinkSource(): ImportSource | '' {
    const q = page.url.searchParams.get('source') ?? '';
    if (!isImportSource(q)) return '';
    return IMPORT_STRATEGIES[q].available ? q : '';
  }

  function applyDeepLink() {
    const linked = deepLinkSource();
    if (!linked) return;
    if (session.source !== linked) {
      session.reset();
      session.choose(linked);
      session.stage = 'instructions';
    }
    session.previewError = deepLinkError();
  }

  function deepLinkError(): string {
    const e = page.url.searchParams.get('e');
    const spec = session.source ? IMPORT_STRATEGIES[session.source].input : null;
    if (!e || spec?.kind !== 'oauth') return '';
    const key = spec.errorParams[e];
    return key ? t(key) : '';
  }

  const source = $derived(session.source);
  const strategy = $derived<ImportSourceStrategy | null>(source ? IMPORT_STRATEGIES[source] : null);
  const inputSpec = $derived<InputSpec | null>(strategy?.input ?? null);

  const connected = $derived((page.data.connected ?? {}) as Partial<Record<ImportSource, boolean>>);
  const sourceConnected = $derived(source ? connected[source] === true : false);
  let dragKind = $state<'' | ImportSource>('');
  let finishError = $state('');
  let finishing = $state(false);

  const previewResult = $derived(session.previewResult);
  const commitResult = $derived(session.commitResult);
  const submitting = $derived(session.submitting);
  const previewError = $derived(session.previewError);
  const commitError = $derived(session.commitError);
  const uploadFile = $derived(session.uploadFile);

  const unnumbered = (s: string) => s.replace(/^\s*\d+\s*·\s*/, '');
  const stepIndex = $derived(ORDER.indexOf(step));
  const journeyStep = $derived(BEFORE_IMPORT.length + stepIndex);
  const journeyLabels = $derived([...BEFORE_IMPORT, ...STAGES]);
  const maxRailStep = $derived(stepIndex);
  const stepAnnouncement = $derived(
    t('onboarding.stepAnnounce', {
      n: journeyStep + 1,
      total: journeyLabels.length,
      label: journeyLabels[journeyStep]
    })
  );

  let leaveTarget = $state<number | null>(null);
  const hasDraft = $derived(session.hasProgress || !!session.credential || !!session.uploadFile);

  function selectRail(i: number) {
    if (i < BEFORE_IMPORT.length) {
      if (hasDraft) leaveTarget = i;
      else onback(i + 1);
      return;
    }
    const importIndex = i - BEFORE_IMPORT.length;
    const target = ORDER[importIndex];
    if (importIndex <= maxRailStep && (importIndex < 2 || previewResult || target === 'done' && commitResult)) goStep(target);
  }

  function confirmLeave() {
    const target = leaveTarget;
    leaveTarget = null;
    if (target !== null) onback(target + 1);
  }

  const expo = bezier(0.16, 1, 0.3, 1);
  const TRAVEL = 72;
  let dir = $state(1);
  const sceneIn = (node: Element) =>
    prefersReducedMotion()
      ? { duration: 0 }
      : fly(node, { x: dir * TRAVEL, duration: 720, delay: 140, easing: expo, opacity: 0 });
  const sceneOut = (node: Element) =>
    prefersReducedMotion()
      ? { duration: 0 }
      : fly(node, { x: -dir * 56, duration: 360, easing: expo, opacity: 0 });

  type Sequence = 'entrance' | 'burst' | 'orbit' | 'comet';
  const STAGE_FACE: Record<Step, string> = {
    pick: 'curious',
    instructions: 'attentive',
    commands: 'excited',
    extras: 'happy',
    review: 'attentive',
    done: 'proud'
  };
  let sequence = $state<Sequence>('entrance');
  let sequenceKey = $state(0);
  let reaction = $state<string | null>(null);
  let hover = $state<string | null>(null);
  let reactionTimer: ReturnType<typeof setTimeout> | null = null;
  const expression = $derived(reaction ?? hover ?? STAGE_FACE[step]);
  function play(seq: Sequence) {
    sequence = seq;
    sequenceKey += 1;
  }
  function react(face: string, ms: number) {
    reaction = face;
    if (reactionTimer) clearTimeout(reactionTimer);
    reactionTimer = setTimeout(() => (reaction = null), ms);
  }

  function goStep(target: Step) {
    if (target === step) return;
    dir = ORDER.indexOf(target) > stepIndex ? 1 : -1;
    hover = null;
    session.stage = target;
    play('entrance');
  }

  let flowEl = $state<HTMLElement | null>(null);
  let focusedStep: Step | null = null;
  let hydrated = false;
  let finished = false;

  function focusHeading(target: Step) {
    const heading = flowEl?.querySelector<HTMLElement>(`.scene[data-step="${target}"] h2`);
    if (!heading) return;
    heading.tabIndex = -1;
    heading.focus({ preventScroll: true });
  }

  $effect(() => {
    const current = step;
    if (focusedStep === null || current === focusedStep) {
      focusedStep = current;
      return;
    }
    focusedStep = current;
    tick().then(() => focusHeading(current));
  });

  $effect(() => {
    session.stage;
    session.source;
    session.previewResult;
    session.commitResult;
    if (hydrated) untrack(() => saveSnapshot(session.snapshot()));
  });

  $effect(() => {
    const snap = { selected: session.selected, overwrite: session.overwrite };
    if (hydrated) saveSelection(snap);
  });

  onMount(() => {
    if (!resumed) {
      const saved = loadSnapshot(ORDER);
      if (saved) session.restore(saved);
      applyDeepLink();
    }
    focusedStep = session.stage as Step;
    hydrated = true;
    return () => {
      if (reactionTimer) clearTimeout(reactionTimer);
      if (!finished) retainSnapshot(session.snapshot());
    };
  });

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

  function choose(s: ImportSource) {
    session.choose(s);
    react('excited', 1400);
    goStep('instructions');
  }

  function reset() {
    goStep('pick');
    session.reset();
  }

  const railDetail = $derived.by(() => [
    strategy ? strategy.label : t('import.railPickPending'),
    session.inputDetail(inputSpec),
    previewResult ? t('onboardingImport.commandsCount', { n: previewResult.manifest?.commands?.length ?? 0 }) : '',
    previewResult ? session.statsLine : '',
    previewResult ? session.selectionLine : '',
    commitResult ? t('import.railDone') : ''
  ]);

  const reviewHint = $derived.by(() => {
    if (!strategy) return '';
    let s = t('import.reviewHint', { source: strategy.label, stats: session.statsLine });
    if (session.fatalCount > 0) s += ' ' + t('import.fatalSuffix', { n: session.fatalCount });
    return s;
  });

  function normalizeName(n: string): string {
    return n.trim().replace(/^!/, '').trim().toLowerCase();
  }

  const instrSteps = $derived.by(() => {
    const key = strategy?.i18n.instr;
    return key ? tl(key) : [];
  });

  async function runPreview() {
    const result = await session.runPreview();
    if (result === 'failed') react('attentive', 2400);
    if (result !== 'ok') return;
    react('proud', 1800);
    goStep(previewResult?.manifest?.commands?.length ? 'commands' : 'extras');
  }

  async function runCommit() {
    const result = await session.runCommit();
    if (result === 'failed') react('attentive', 2400);
    if (result !== 'ok') return;
    react('love', 2200);
    goStep('done');
    play('burst');
  }

  let showCompletion = $state(false);
  let finishSaved: Promise<boolean> = Promise.resolve(false);
  let finishDestination = '/';

  function finishOnboarding(to: string) {
    if (finishing) return;
    finishing = true;
    finishDestination = to;
    finishError = '';
    react('proud', 4000);
    play('burst');
    finishSaved = saveFinish();
    showCompletion = true;
  }

  async function saveFinish(): Promise<boolean> {
    try {
      const body = new FormData();
      body.set('consent', consentAccepted ? 'yes' : 'no');
      const res = await fetch('/welcome?/finishImport', { method: 'POST', body });
      const result = deserialize(await res.text());
      if (result.type === 'success' && (result.data as { ok?: boolean } | undefined)?.ok) return true;
    } catch {
    }
    finishFailed(t('onboardingImport.saveFailed'));
    return false;
  }

  function finishFailed(message: string) {
    finishError = message;
    showCompletion = false;
    finishing = false;
    react('attentive', 2400);
  }

  async function afterCompletion() {
    if (!(await finishSaved)) return;
    finished = true;
    clearSnapshot();
    onexit(finishDestination);
  }

  function pickFile(f: File | null | undefined) {
    const result = session.pickFile(f, inputSpec);
    if (result === 'rejected') react('attentive', 2400);
    if (result === 'picked') react('happy', 1600);
  }
</script>

<svelte:head><title>{t('onboardingImport.pageTitle')} · ItsBagelBot</title><meta name="robots" content="noindex, nofollow" /></svelte:head>
<svelte:window onpointermove={onPointerMove} />
<Sky
  shift={stepIndex % 2 ? 1 : -1}
  turn={stepIndex * 24}
  {px}
  {py}
  progress={journeyStep / (journeyLabels.length - 1)}
  leaving={showCompletion}
/>
<div class="welcome-import" class:leaving={showCompletion} data-orbs="off">
  <header class="top">
    <Brand title="ItsBagelBot" sub={t('common.console')} logoSrc="/logo.png" logoAlt="" size="md" />
    <StepRail labels={journeyLabels} current={journeyStep} maxStep={journeyStep} label={t('onboarding.stepOf', { n: journeyStep + 1, total: journeyLabels.length })} onselect={selectRail} />
  </header>
<section class="screen active">
  <div class="intro">
    <div class="companion" class:flip={stepIndex % 2 === 1} aria-hidden="true">
      <div class="companion-blob">
        <span class="companion-plate"></span>
        <span class="companion-scale">
        <Bolota
          name={page.data.name ?? 'ItsBagelBot'}
          size={130}
          active
          follow
          cycle={false}
          {expression}
          {sequence}
          {sequenceKey}
          sequenceFor={1000}
          sequenceHold={200}
        />
        </span>
      </div>
      <div class="bubble">
        {#key step}
          <span class="bubble-inner" in:sceneIn out:sceneOut>
            <span class="bubble-stage">{String(journeyStep + 1).padStart(2, '0')} · {STAGES[stepIndex]}</span>
            {#if railDetail[stepIndex]}<span class="bubble-detail">{railDetail[stepIndex]}</span>{/if}
          </span>
        {/key}
      </div>
    </div>
  </div>

  <div class="wizard">
    <div class="flow" bind:this={flowEl}>
  {#key step}
  <div class="scene" data-step={step} in:sceneIn out:sceneOut>

  {#if step === 'pick'}
    <Card glass>
      <Heading level={2} class="step-title">{unnumbered(t('import.stepPick'))}</Heading>
      <p class="hint">{t('import.pickHint')}</p>

      <div class="tiles">
        {#each IMPORT_SOURCES as id, ti (id)}
          {@const s = IMPORT_STRATEGIES[id]}
          {#if s.available}
            <label
              class="tile"
              class:picked={source === id}
              data-cursor
              style="--ti: {ti};"
              onpointerenter={() => (hover = 'excited')}
              onpointerleave={() => (hover = null)}
            >
              <input
                type="radio"
                name="source-pick"
                value={id}
                checked={source === id}
                onchange={() => choose(id)}
              />
              <span class="tile-top">
                <span class="glyph" aria-hidden="true">{s.initials}</span>
                <Tag tone="pre">{t(CHIP_LABEL_KEYS[s.chip])}</Tag>
              </span>
              <span class="tile-name">{s.label}</span>
              <span class="tile-desc">{t(s.i18n.desc)}</span>
              <span class="tile-foot">
                <span class="tile-cta">{t('import.tileCta')}</span>
              </span>
            </label>
          {:else}
            <div class="tile disabled" aria-disabled="true" style="--ti: {ti};">
              <span class="tile-top">
                <span class="glyph" aria-hidden="true">{s.initials}</span>
                <Tag tone="quiet">{t('import.chipSoon')}</Tag>
              </span>
              <span class="tile-name">{s.label}</span>
              <span class="tile-desc">{t(s.i18n.desc)}</span>
            </div>
          {/if}
        {/each}
      </div>
      <div class="actions">
        <Button variant="ghost" type="button" onclick={() => onback(4)}>{t('onboarding.back')}</Button>
      </div>
    </Card>
  {:else if step === 'instructions' && source}
    {@const st = IMPORT_STRATEGIES[source]}
    {@const spec = st.input}
    <Card glass>
      <div class="instr-head">
        <span class="glyph" aria-hidden="true">{st.initials}</span>
        <Heading level={2} class="step-title">{unnumbered(t('import.stepInstructions', { source: st.label }))}</Heading>
      </div>
      <p class="hint">{t('import.instrHint', { source: st.label })}</p>

      {#if instrSteps.length}
        <ol class="steps">
          {#each instrSteps as s, i (i)}<li>{s}</li>{/each}
        </ol>
      {/if}

      {#if spec.kind === 'text'}
        {#if spec.linkHref && spec.linkLabel}
          <p class="instr-link bb-prose">
            <a href={spec.linkHref} target="_blank" rel="noopener noreferrer">{t(spec.linkLabel)}</a>
          </p>
        {/if}
        <div class="cred">
          {#if spec.secret}
            <Textarea
              rows={3}
              fill
              mono
              placeholder={spec.placeholder}
              bind:value={session.credential}
              spellcheck="false"
              autocomplete="off"
              autocapitalize="off"
              aria-label={t(spec.i18n.field)}
            />
          {:else}
            <input
              class="bb-input bb-input--fill cred-mono"
              type="text"
              placeholder={spec.placeholder}
              bind:value={session.credential}
              maxlength={spec.maxLen}
              spellcheck="false"
              autocomplete="off"
              autocapitalize="off"
              aria-label={t(spec.i18n.field)}
            />
          {/if}
          {#if spec.i18n.hint}
            <p class="hint">
              {@html t(spec.i18n.hint)}
            </p>
          {/if}
        </div>
      {:else if spec.kind === 'oauth'}
        <div class="cred">
          {#if sourceConnected}
            <p class="nb-connected" role="status">
              {t(spec.i18n.connected)}
            </p>
          {:else}
            <ButtonLink href={`${spec.connectPath}?return=welcome`} variant="primary" class="cred-cta">
              {t(spec.i18n.cta)}
            </ButtonLink>
          {/if}
          <p class="hint">{t(spec.i18n.scopeHint)}</p>
        </div>
      {:else}
        <span
          class="drop"
          class:over={dragKind === source}
          class:has-file={!!uploadFile}
        >
          <input
            type="file"
            accept={spec.accept}
            onchange={(e) => pickFile(e.currentTarget.files?.[0])}
            ondragover={(e) => {
              e.preventDefault();
              dragKind = source;
              hover = 'surprised';
            }}
            ondragleave={() => {
              dragKind = '';
              hover = null;
            }}
            ondrop={(e) => {
              e.preventDefault();
              dragKind = '';
              hover = null;
              pickFile(e.dataTransfer?.files?.[0]);
            }}
          />
          {uploadFile ? uploadFile.name : t('import.dropHint')}
        </span>
      {/if}

      {#if previewError}<AlertBanner>{previewError}</AlertBanner>{/if}

      <form
        class="actions"
        onsubmit={(e) => {
          e.preventDefault();
          runPreview();
        }}
      >
        <div class="actions-row">
          <Button variant="ghost" type="button" onclick={() => (submitting ? session.cancel() : goStep('pick'))}
            >{submitting ? t('common.cancel') : t('import.back')}</Button
          >
          <Button type="submit" variant="primary" loading={submitting}>
            {t('import.continueCta')}
          </Button>
        </div>
      </form>
    </Card>
  {:else if (step === 'commands' || step === 'extras' || step === 'review') && previewResult?.manifest}
    {#if step === 'review'}
    <Card glass class="review-head">
      <Heading level={2} class="step-title">{unnumbered(t('import.reviewTitle'))}</Heading>
      <p class="hint">{reviewHint}</p>

      <div class="review-bar">
        {#each session.statChips as c (c)}<Tag tone="quiet">{c}</Tag>{/each}
        <span class="review-spacer"></span>
        <Button type="button" variant="ghost" size="sm" onclick={() => session.setAll(true)}>{t('import.selectAll')}</Button>
        <Button type="button" variant="ghost" size="sm" onclick={() => session.setAll(false)}>{t('import.selectNone')}</Button>
      </div>
    </Card>
    {:else}
      <div class="category-head">
        <span class="eyebrow">{step === 'commands' ? t('onboardingImport.commandsEyebrow') : t('onboardingImport.extrasEyebrow')}</span>
        <Heading level={2} class="category-title">{step === 'commands' ? t('onboardingImport.commandsTitle') : t('onboardingImport.extrasTitle')}</Heading>
        <Text tone="muted">{step === 'commands' ? t('onboardingImport.commandsBody') : t('onboardingImport.extrasBody')}</Text>
      </div>
      {#if step === 'extras'}
        <div class="module-note" role="note">
          <strong>{t('onboardingImport.modulesTitle')}</strong>
          <Text tone="muted" class="module-note-body">{t('onboardingImport.modulesBody')}</Text>
        </div>
      {/if}
    {/if}

      {#each session.manifestLevelDiags as d (d.code + d.message)}
        <p class="manifest-warn" role="status">{d.message}</p>
      {/each}

      {#if session.anyCollisions}
        <div class="collision-note">
          {@html t('import.conflictsNote', { n: previewResult.collisions?.length ?? 0 })}
          <span class="overwrite-toggle">
            <Checkbox bind:checked={session.overwrite} name="overwrite" value="on">{t('import.overwriteToggle')}</Checkbox>
          </span>
        </div>
      {/if}

      {#if step === 'commands' && previewResult.manifest.commands?.length}
        <Card glass class="group">
          <div class="group-head">
            <span class="group-title">{t('import.hCommands')}</span>
            <span class="group-count">{previewResult.manifest.commands.length}</span>
          </div>
          <ul class="rows">
            {#each previewResult.manifest.commands as c, i (c.name)}
              {@const diags = session.diagsFor('commands', i)}
              <li class="row-item" class:collision={session.collidedCommands.has(normalizeName(c.name))}>
                <span class="pick">
                  <Checkbox bind:checked={() => session.isPicked('commands', i), (on) => session.toggle('commands', i, on)}><span class="row-name">!{c.name}</span></Checkbox>
                </span>
                <div class="row-body">
                  <span class="row-response">{c.responses?.join(' / ')}</span>
                  <span class="chips">
                    {#if c.permission && c.permission !== 'everyone'}<PermBadge perm={c.permission} />{/if}
                    {#if c.cooldown_seconds}<Tag tone="bare">{t('import.cooldownChip', { n: c.cooldown_seconds })}</Tag>{/if}
                    {#each c.aliases ?? [] as a (a)}<Tag tone="bare" class="bb-tag--literal">!{a}</Tag>{/each}
                    {#each diags.filter((d) => d.severity === 'warn') as d (d.code + d.message)}
                      <Tag tone="alpha" title={d.message}>{d.message}</Tag>
                    {/each}
                    {#each diags.filter((d) => d.severity === 'error') as d (d.code + d.message)}
                      <Tag tone="error" title={d.message}>{t('import.cannotImport', { m: d.message })}</Tag>
                    {/each}
                    {#if session.collidedCommands.has(normalizeName(c.name))}
                      <Tag tone="error">{t('import.alreadyExists')}</Tag>
                    {/if}
                  </span>
                </div>
              </li>
            {/each}
          </ul>
        </Card>
      {/if}

      {#if step === 'extras' && previewResult.manifest.timers?.length}
        <Card glass class="group">
          <div class="group-head">
            <span class="group-title">{t('import.hTimers')}</span>
            <span class="group-count">{previewResult.manifest.timers.length}</span>
          </div>
          <ul class="rows">
            {#each previewResult.manifest.timers as tm, i (tm.message)}
              {@const diags = session.diagsFor('timers', i)}
              <li class="row-item">
                <span class="pick">
                  <Checkbox bind:checked={() => session.isPicked('timers', i), (on) => session.toggle('timers', i, on)} aria-label={tm.message} />
                </span>
                <div class="row-body">
                  <span class="row-response">{tm.message}</span>
                  <span class="chips">
                    <Tag tone="bare">{t('import.everySeconds', { n: tm.interval_seconds })}</Tag>
                    {#each diags.filter((d) => d.severity === 'warn') as d (d.code + d.message)}
                      <Tag tone="alpha" title={d.message}>{d.message}</Tag>
                    {/each}
                    {#each diags.filter((d) => d.severity === 'error') as d (d.code + d.message)}
                      <Tag tone="error" title={d.message}>{t('import.cannotImport', { m: d.message })}</Tag>
                    {/each}
                  </span>
                </div>
              </li>
            {/each}
          </ul>
        </Card>
      {/if}

      {#if step === 'extras' && previewResult.manifest.triggers?.length}
        <Card glass class="group">
          <div class="group-head">
            <span class="group-title">{t('import.hTriggers')}</span>
            <span class="group-count">{previewResult.manifest.triggers.length}</span>
          </div>
          <ul class="rows">
            {#each previewResult.manifest.triggers as tg, i (tg.phrase)}
              {@const diags = session.diagsFor('triggers', i)}
              <li class="row-item">
                <span class="pick">
                  <Checkbox bind:checked={() => session.isPicked('triggers', i), (on) => session.toggle('triggers', i, on)}><span class="row-name">{tg.phrase}</span></Checkbox>
                </span>
                <div class="row-body">
                  <span class="row-response">{tg.response}</span>
                  <span class="chips">
                    {#each diags.filter((d) => d.severity === 'warn') as d (d.code + d.message)}
                      <Tag tone="alpha" title={d.message}>{d.message}</Tag>
                    {/each}
                    {#each diags.filter((d) => d.severity === 'error') as d (d.code + d.message)}
                      <Tag tone="error" title={d.message}>{t('import.cannotImport', { m: d.message })}</Tag>
                    {/each}
                  </span>
                </div>
              </li>
            {/each}
          </ul>
        </Card>
      {/if}

      {#if step === 'extras' && previewResult.manifest.quotes?.length}
        <Card glass class="group">
          <div class="group-head">
            <span class="group-title">{t('import.hQuotes')}</span>
            <span class="group-count">{previewResult.manifest.quotes.length}</span>
          </div>
          <p class="hint">{t('import.quotesAll', { n: previewResult.manifest.quotes.length })}</p>
        </Card>
      {/if}

      {#if commitError}<ImportAlert message={commitError} />{/if}

      {#if step === 'review'}
      <form
        class="commit-bar"
        onsubmit={(e) => {
          e.preventDefault();
          runCommit();
        }}
      >
        <span class="commit-line">{session.selectionLine}</span>
        <div class="actions-row">
          <Button variant="ghost" type="button" onclick={() => (submitting ? session.cancel() : reset())}
            >{submitting ? t('common.cancel') : t('import.startOver')}</Button
          >
          <Button type="submit" variant="primary" loading={submitting}>
            {t('import.importNow')}
          </Button>
        </div>
      </form>
      {:else}
        <div class="category-actions">
          <Button variant="ghost" type="button" onclick={() => goStep(step === 'extras' && previewResult?.manifest?.commands?.length ? 'commands' : 'instructions')}>{t('import.back')}</Button>
          <Button variant="primary" type="button" onclick={() => goStep(step === 'commands' ? 'extras' : 'review')}>{t('onboardingImport.continue')}</Button>
        </div>
      {/if}
  {:else if step === 'done'}
    <Card glass class="done-panel">
      <span class="done-seal" aria-hidden="true"><Icon name="check" size={22} /></span>
      <Heading level={2} class="step-title">{unnumbered(t('import.doneTitle'))}</Heading>
      {#if commitResult}
        <p class="hint">
          {t('import.doneLine', {
            c: commitResult.applied.commands,
            tm: commitResult.applied.timers,
            tg: commitResult.applied.triggers,
            q: commitResult.applied.quotes
          })}
        </p>
        <ImportSkipped skipped={commitResult.skipped} {t} />
        {#if session.appliedTiles.length}
          <div class="applied">
            {#each session.appliedTiles as a (a.label)}
              <div class="applied-tile">
                <span class="applied-n">{a.n}</span>
                <span class="applied-label">{a.label}</span>
              </div>
            {/each}
          </div>
        {/if}
        {#each commitResult.diagnostics ?? [] as d (d.code + d.message)}
          <p class:manifest-warn={d.severity === 'warn'} class:form-error={d.severity === 'error'} role="status">
            {d.message}
          </p>
        {/each}
      {:else}
        <p class="hint">{t('import.nothingApplied')}</p>
      {/if}
      <div class="actions actions-row done-actions">
        <Button variant="green" solid onclick={() => finishOnboarding('/')} loading={finishing}>{t('onboardingImport.dashboard')}</Button>
        <Button variant="ghost" onclick={() => finishOnboarding('/commands')} disabled={finishing}>{t('import.reviewCommands')}</Button>
        <Button variant="ghost" onclick={reset} disabled={finishing}>{t('import.importAnother')}</Button>
      </div>
      {#if finishError}<ImportAlert message={finishError} />{/if}
      {#if commitResult?.audit_id}
        <p class="audit">{t('import.auditFoot', { n: commitResult.audit_id })}</p>
      {/if}
    </Card>
  {/if}
  </div>
  {/key}
    </div>
  </div>
</section>
</div>
<VisuallyHidden as="p" role="status">{stepAnnouncement}</VisuallyHidden>
<ConfirmDialog
  open={leaveTarget !== null}
  title={t('import.leaveTitle')}
  body={t('import.leaveBody')}
  confirmLabel={t('common.leave')}
  cancelLabel={t('import.leaveStay')}
  onCancel={() => (leaveTarget = null)}
  onConfirm={confirmLeave}
/>
{#if showCompletion}<Completion label={t('onboardingImport.stageDone')} oncomplete={afterCompletion} />{/if}

<style>
  .welcome-import {
    position: relative;
    z-index: 1;
    min-height: 100vh;
    padding: 0 clamp(18px, 5vw, 72px) 72px;
    color: var(--bb-white);
    overflow-x: clip;
  }
  .top { animation: settle 900ms var(--bb-ease-out-expo) backwards; }
  :global(.welcome-import .screen) { animation: arrive-far 760ms var(--bb-ease-out-expo) backwards; }
  .leaving .top,
  .leaving :global(.screen) {
    opacity: 0;
    transition: opacity 400ms var(--bb-ease-out-expo);
  }
  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 24px;
    min-height: 96px;
  }
  :global(.welcome-import .screen) {
    width: min(1120px, 100%);
    margin: 24px auto 0;
  }
  .intro {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 32px;
    margin-bottom: 12px;
  }

  .companion {
    --blob: 156px;
    --bubble: 220px;
    flex: none;
    position: relative;
    display: flex;
    align-items: center;
    gap: 14px;
    width: calc(var(--blob) + var(--bubble) + 14px);
  }
  .companion-blob {
    position: relative;
    flex: none;
    width: var(--blob);
    height: var(--blob);
    display: grid;
    place-items: center;
    filter: drop-shadow(0 14px 26px rgba(0, 0, 0, .38));
    transition: transform 1000ms var(--bb-ease-out-expo);
  }
  .companion-scale {
    display: grid;
    place-items: center;
    animation: float 9s ease-in-out infinite;
  }
  .companion-plate {
    position: absolute;
    inset: -8%;
    z-index: -1;
    border-radius: 50%;
    background: radial-gradient(
      circle at 40% 35%,
      rgba(var(--bb-green-glow-rgb), .3),
      rgba(var(--bb-tan-rgb), .14) 46%,
      transparent 70%
    );
    filter: blur(18px);
    animation: breathe 6s ease-in-out infinite;
  }
  .bubble {
    position: relative;
    display: grid;
    width: var(--bubble);
    min-height: 64px;
    padding: 12px 16px;
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-md);
    background: rgba(17, 17, 16, .7);
    backdrop-filter: blur(14px);
    overflow: hidden;
    transition: transform 1000ms var(--bb-ease-out-expo);
  }
  .bubble-inner { grid-area: 1 / 1; display: grid; gap: 4px; align-content: center; }
  .bubble-stage {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: .14em;
    text-transform: uppercase;
    color: var(--bb-tan-pale);
  }
  .bubble-detail {
    font-size: 12.5px;
    line-height: 1.4;
    color: var(--bb-muted);
    overflow-wrap: anywhere;
  }
  .companion.flip .companion-blob { transform: translateX(calc(var(--bubble) + 14px)); }
  .companion.flip .bubble { transform: translateX(calc(-1 * (var(--blob) + 14px))); }
  .category-head :global(.category-title) {
    margin-bottom: 12px;
    font-weight: 700;
    font-size: clamp(25px, 3vw, 36px);
    letter-spacing: -.04em;
    line-height: 1.08;
  }
  .category-head {
    padding: 26px 30px;
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-lg);
    background: rgba(17, 17, 16, .76);
    backdrop-filter: blur(18px);
  }
  .module-note {
    padding: 18px 22px;
    border: 1px solid rgba(201, 168, 124, .38);
    border-radius: var(--bb-radius-md);
    background: rgba(201, 168, 124, .07);
  }
  .module-note strong { color: var(--bb-tan-pale); }
  .module-note :global(.module-note-body) { margin-top: 6px; line-height: 1.55; }
  .category-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    padding-top: 8px;
  }
  :global(:root[data-theme="light"]) .bubble,
  :global(:root[data-theme="light"]) .category-head {
    background: rgba(255, 255, 255, .72);
  }

  @media (max-width: 760px) {
    .top { flex-wrap: wrap; padding: 18px 0; }
    :global(.welcome-import .screen) { margin-top: 24px; }
  }
  @media (max-width: 1000px) {
    .intro {
      flex-direction: column-reverse;
      align-items: stretch;
      gap: 18px;
      margin-bottom: 24px;
    }
    .companion {
      --blob: 84px;
      --bubble: calc(100% - 98px);
      width: 100%;
    }
    .companion-scale { scale: .64; }
    .companion.flip .companion-blob,
    .companion.flip .bubble { transform: none; }
  }

  :global(.step-title) { margin-bottom: 6px; font-size: 16px; }
  .hint {
    color: var(--bb-muted, #888077);
    font-size: 13px;
    margin: 0 0 12px;
  }

  .wizard {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-items: start;
  }
  .flow {
    min-width: 0;
    display: grid;
  }
  .scene {
    grid-area: 1 / 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 12px;
    margin: 16px 0 4px;
  }
  .tile {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 10px;
    min-height: 176px;
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-md);
    padding: 18px;
    background: linear-gradient(180deg, rgba(255, 255, 255, 0.035), rgba(0, 0, 0, 0.18));
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.05);
    cursor: pointer;
    transition:
      border-color 200ms ease,
      background 200ms ease,
      box-shadow 200ms ease;
    animation: tile-in 640ms var(--bb-ease-out-expo) calc(320ms + var(--ti, 0) * 70ms) both;
  }
  .tile:not(.disabled):hover {
    border-color: rgba(201, 168, 124, 0.45);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.07),
      0 14px 30px rgba(0, 0, 0, 0.28);
  }
  .tile:not(.disabled):hover .glyph { transform: translateX(3px); }
  .tile-cta { transition: transform 420ms var(--bb-ease-out-expo); }
  .tile:not(.disabled):hover .tile-cta { transform: translateX(4px); }
  .tile.picked {
    border-color: rgba(201, 168, 124, 0.65);
    background: linear-gradient(180deg, rgba(201, 168, 124, 0.08), rgba(0, 0, 0, 0.18));
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.07),
      0 0 0 1px rgba(201, 168, 124, 0.35),
      0 10px 26px rgba(0, 0, 0, 0.22);
  }
  .tile.disabled {
    cursor: default;
    opacity: 0.55;
  }
  .tile input[type='radio'] {
    position: absolute;
    opacity: 0;
    pointer-events: none;
  }
  .tile:has(input[type='radio']:focus-visible) {
    outline: 2px solid var(--bb-green-glow, #52b788);
    outline-offset: 2px;
  }
  .tile-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 4px;
  }
  .tile-foot {
    display: flex;
    align-items: center;
    margin-top: auto;
    padding-top: 12px;
    border-top: 1px solid var(--glass-border);
  }
  .glyph {
    flex: none;
    width: 34px;
    height: 34px;
    border-radius: var(--bb-radius-sm);
    border: 1px solid var(--glass-border);
    background: rgba(255, 255, 255, 0.04);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 12px;
    letter-spacing: 0.02em;
    color: var(--bb-tan-light);
    transition:
      border-color 200ms ease,
      color 200ms ease,
      transform 420ms var(--bb-ease-out-expo);
  }
  .tile.picked .glyph {
    border-color: rgba(201, 168, 124, 0.55);
    color: var(--bb-tan);
  }
  .tile-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    line-height: 1.25;
    color: var(--bb-white);
    overflow-wrap: break-word;
  }
  .tile-desc {
    color: var(--bb-muted);
    font-size: 13px;
    line-height: 1.5;
  }

  .steps {
    margin: 6px 0 16px;
    padding-left: 22px;
    display: flex;
    flex-direction: column;
    gap: 9px;
  }
  .steps li {
    font-size: 13.5px;
    line-height: 1.55;
    color: var(--bb-white);
    overflow-wrap: anywhere;
  }
  .instr-link {
    margin: 0 0 14px;
    font-size: 13.5px;
  }
  .instr-link a { text-decoration: underline; text-underline-offset: 2px; }
  .cred {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-bottom: 6px;
  }
  :global(.cred-cta) {
    align-self: flex-start;
  }
  .nb-connected {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0;
    color: var(--bb-green, #7dc98f);
    font-size: 13px;
  }
  .cred-mono { font-family: var(--bb-font-mono); }
  .drop {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 64px;
    padding: 10px 34px;
    border: 1px dashed var(--glass-border);
    border-radius: var(--bb-radius-md);
    background: rgba(255, 255, 255, 0.03);
    color: var(--bb-muted);
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    letter-spacing: 0.04em;
    text-align: center;
    overflow-wrap: anywhere;
    transition:
      border-color 160ms ease,
      background 160ms ease,
      color 160ms ease;
  }
  .drop:hover {
    border-color: var(--bb-tan);
    color: var(--bb-tan-pale);
  }
  .drop.over {
    border-color: var(--bb-tan);
    background: rgba(201, 168, 124, 0.08);
    color: var(--bb-white);
  }
  .drop.has-file {
    color: var(--bb-white);
  }
  .drop input[type='file'] {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    opacity: 0;
    cursor: pointer;
  }
  .drop:focus-within {
    outline: 2px solid var(--bb-green-glow, #52b788);
    outline-offset: 2px;
  }

  .done-actions { flex-wrap: wrap; min-height: 44px; }
  .scene :global(h2:focus) { outline: none; }
  .actions {
    margin-top: 20px;
  }
  .actions-row {
    display: flex;
    gap: 10px;
    justify-content: flex-end;
  }
  .form-error {
    color: #e5484d;
    font-size: 13px;
    margin: 10px 0 0;
  }
  .manifest-warn {
    color: var(--bb-tan-light);
    font-size: 13px;
    margin: 8px 0 0;
  }

  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .row-item {
    display: flex;
    align-items: baseline;
    gap: 14px;
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-sm);
    padding: 12px 14px;
    background: var(--glass-fill);
  }
  .row-item.collision {
    border-color: rgba(229, 72, 77, 0.55);
  }
  .pick {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    white-space: nowrap;
    --bb-check-align: center;
  }
  .row-name {
    font-family: var(--bb-font-mono);
    font-size: 13px;
    color: var(--bb-white);
  }
  .row-body {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }
  .row-response {
    color: var(--bb-muted);
    font-size: 13px;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
  }

  .collision-note {
    border: 1px solid rgba(229, 72, 77, 0.35);
    background: rgba(229, 72, 77, 0.06);
    border-radius: var(--bb-radius-md);
    padding: 12px 14px;
    font-size: 13px;
    line-height: 1.5;
    color: var(--bb-white);
    margin: 0 0 14px;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
  }
  .overwrite-toggle {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    margin-left: auto;
    --bb-check-align: center;
    font-size: 13px;
    white-space: nowrap;
  }

  @media (max-width: 560px) {
    .collision-note {
      flex-direction: column;
      align-items: flex-start;
    }
    .overwrite-toggle {
      margin-left: 0;
    }
  }
  .tile-cta {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-green-glow, #52b788);
  }

  .instr-head {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 14px;
  }
  .instr-head :global(.step-title) {
    margin-bottom: 0;
  }

  .review-bar {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
  }
  .review-spacer {
    flex: 1;
  }

  :global(.group) {
    padding: 0;
    overflow: hidden;
  }
  .group-head {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 16px 22px;
    border-bottom: 1px solid var(--bb-border);
  }
  .group-title {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--bb-tan, #c9a87c);
  }
  .group-count {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: #5f5a53;
  }
  :global(.group) .rows {
    margin: 0;
  }
  :global(.group) .hint {
    margin: 0;
    padding: 16px 22px;
  }
  :global(.group) .row-item {
    padding: 16px 22px;
  }

  .commit-bar {
    position: sticky;
    bottom: 18px;
    display: flex;
    flex-wrap: wrap;
    gap: 14px;
    align-items: center;
    justify-content: space-between;
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-pill);
    background: rgba(17, 17, 16, 0.92);
    backdrop-filter: blur(18px);
    padding: 14px 16px 14px 24px;
    box-shadow: 0 8px 30px rgba(0, 0, 0, 0.35);
  }
  .commit-line {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  .done-seal {
    display: inline-grid;
    place-items: center;
    width: 52px;
    height: 52px;
    margin-bottom: 14px;
    border-radius: 50%;
    border: 1px solid rgba(var(--bb-green-glow-rgb), .5);
    background: rgba(var(--bb-green-glow-rgb), .14);
    color: var(--bb-green-glow);
    box-shadow: 0 0 0 6px rgba(var(--bb-green-glow-rgb), .06);
    animation: seal-in 700ms var(--bb-ease-out-expo) 360ms both;
  }
  .applied-tile { animation: tile-in 640ms var(--bb-ease-out-expo) 520ms both; }
  .applied-tile:nth-child(2) { animation-delay: 590ms; }
  .applied-tile:nth-child(3) { animation-delay: 660ms; }
  .applied-tile:nth-child(4) { animation-delay: 730ms; }
  .applied-tile:nth-child(5) { animation-delay: 800ms; }
  .applied {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
    gap: 12px;
    margin: 22px 0 26px;
  }
  .applied-tile {
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-md);
    padding: 16px 18px;
    background: var(--glass-fill);
  }
  .applied-n {
    display: block;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 26px;
    color: var(--bb-white);
  }
  .applied-label {
    display: block;
    margin-top: 6px;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.16em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .audit {
    margin: 24px 0 0;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.1em;
    color: #5f5a53;
  }

  @media (max-width: 560px) {
    .commit-bar {
      border-radius: var(--bb-radius-sm);
      padding: 14px 16px;
    }
  }

  @keyframes settle {
    from { opacity: 0; transform: translateX(-16px); }
    to { opacity: 1; transform: none; }
  }
  @keyframes arrive-far {
    from { opacity: 0; transform: translateX(8vw); }
    to { opacity: 1; transform: none; }
  }
  @keyframes tile-in {
    from { opacity: 0; transform: translateX(22px); }
    to { opacity: 1; transform: none; }
  }
  @keyframes seal-in {
    from { opacity: 0; transform: scale(.4); }
    to { opacity: 1; transform: none; }
  }
  @keyframes float {
    0%, 100% { transform: translate(0, 0); }
    32% { transform: translate(8px, -3px); }
    64% { transform: translate(-6px, 3px); }
  }
  @keyframes breathe {
    0%, 100% { opacity: .7; transform: scale(1); }
    50% { opacity: 1; transform: scale(1.08); }
  }

  @media (prefers-reduced-motion: reduce) {
    .top,
    :global(.welcome-import .screen),
    .companion-scale,
    .companion-plate,
    .tile,
    .done-seal,
    .applied-tile { animation: none; }
    .companion-blob,
    .bubble,
    .glyph,
    .tile-cta { transition: none; }
  }
</style>
