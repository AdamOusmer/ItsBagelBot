<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount, tick, untrack } from 'svelte';
  import { page } from '$app/state';
  import { translate, translateList, type Locale } from '@bagel/kit/i18n';
  import { lateralFly, TRAVEL_IN, TRAVEL_OUT } from '$lib/scene-motion';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import SetupStage from '@bagel/ui/svelte/SetupStage.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import Completion from '$lib/components/welcome/Completion.svelte';
  import ImportAlert from '$lib/import/ImportAlert.svelte';
  import ImportFailed from '$lib/import/ImportFailed.svelte';
  import ImportGroup from '$lib/import/ImportGroup.svelte';
  import ImportRow from '$lib/import/ImportRow.svelte';
  import ImportTimerChips from '$lib/import/ImportTimerChips.svelte';
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
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import FileDrop from '@bagel/ui/svelte/FileDrop.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import RadioGroup from '@bagel/ui/svelte/RadioGroup.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import { PermBadge, Bolota, actionPayload } from '@bagel/kit';
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
  const importText = (text: string, params?: Record<string, string | number>) => translate(locale, `import.${text}`, params);
  const rowText = { cannotImport: (m: string) => t('import.cannotImport', { m }), alreadyExists: () => t('import.alreadyExists') };

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
  const PREVIEW_STEPS = ORDER.slice(ORDER.indexOf('commands'));

  const session = new ImportSession();
  session.configure({ t: importText, isConnected: () => sourceConnected, onPosting: () => play('entrance') });
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
    return key ? importText(key) : '';
  }

  const source = $derived(session.source);
  const strategy = $derived<ImportSourceStrategy | null>(source ? IMPORT_STRATEGIES[source] : null);
  const inputSpec = $derived<InputSpec | null>(strategy?.input ?? null);

  const connected = $derived((page.data.connected ?? {}) as Partial<Record<ImportSource, boolean>>);
  const sourceConnected = $derived(source ? connected[source] === true : false);
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
  const journeySteps = $derived([...BEFORE_IMPORT, ...STAGES].map((label) => ({ label })));
  const maxRailStep = $derived(stepIndex);
  const stepAnnouncement = $derived(
    t('onboarding.stepAnnounce', {
      n: journeyStep + 1,
      total: journeySteps.length,
      label: journeySteps[journeyStep]?.label ?? ''
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
    const target = ORDER[i - BEFORE_IMPORT.length];
    if (railReachable(target)) goStep(target);
  }

  function railReachable(target: Step): boolean {
    const index = ORDER.indexOf(target);
    if (index > maxRailStep) return false;
    if (!PREVIEW_STEPS.includes(target)) return true;
    return !!previewResult || (target === 'done' && !!commitResult);
  }

  function confirmLeave() {
    const target = leaveTarget;
    leaveTarget = null;
    if (target !== null) onback(target + 1);
  }

  let dir = $state(1);
  const sceneIn = (node: Element) => lateralFly(node, { x: dir * TRAVEL_IN, duration: 720, delay: 140 });
  const sceneOut = (node: Element) => lateralFly(node, { x: -dir * TRAVEL_OUT, duration: 360 });

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

  const sourceOptions = $derived(
    IMPORT_SOURCES.map((id) => {
      const s = IMPORT_STRATEGIES[id];
      return {
        value: id,
        label: s.label,
        description: importText(s.i18n.desc),
        meta: s.available ? t('import.tileCta') : undefined,
        disabled: !s.available
      };
    })
  );

  function hoverTile(target: EventTarget | null) {
    const tile = target instanceof Element ? target.closest('label') : null;
    hover = tile && !tile.querySelector('input:disabled') ? 'excited' : null;
  }

  function choose(value: string) {
    if (!isImportSource(value)) return;
    session.choose(value);
    react('excited', 1400);
    goStep('instructions');
  }

  function reset() {
    goStep('pick');
    session.reset();
  }

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
    return key ? translateList(locale, `import.${key}`) : [];
  });

  async function runPreview() {
    const result = await session.runPreview();
    if (result === 'failed') react('attentive', 2400);
    if (result !== 'ok') return;
    react('proud', 1800);
    goStep(hasCommands ? 'commands' : 'extras');
  }

  const hasCommands = $derived(!!previewResult?.manifest?.commands?.length);
  const nextStep = $derived(ORDER[stepIndex + 1]);
  const prevStep = $derived.by(() => {
    const prev = ORDER[stepIndex - 1];
    return prev === 'commands' && !hasCommands ? ORDER[stepIndex - 2] : prev;
  });

  function backFromInstructions() {
    if (submitting) session.cancel();
    else goStep(prevStep);
  }

  function backFromReview() {
    if (submitting) session.cancel();
    else reset();
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
      if (result.type === 'success' && actionPayload(result)?.ok) return true;
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
<SetupStage
  label={t('onboardingImport.pageTitle')}
  steps={journeySteps}
  current={journeyStep}
  maxStep={journeyStep}
  stepLabel={t('onboarding.stepOf', { n: journeyStep + 1, total: journeySteps.length })}
  onSelect={selectRail}
  wide
  leaving={showCompletion}
  class="welcome-import"
  data-orbs="off"
>
  {#snippet start()}<Brand title="ItsBagelBot" sub={t('common.console')} logoSrc="/logo.png" logoAlt="" size="md" />{/snippet}
  {#snippet art()}
    <Bolota name={page.data.name ?? 'ItsBagelBot'} size={240} active follow cycle={false} {expression} {sequence} {sequenceKey} sequenceFor={1000} sequenceHold={200} />
  {/snippet}
    <div class="flow" bind:this={flowEl}>
  {#key step}
  <div class="scene" data-step={step} in:sceneIn out:sceneOut>

  {#if step === 'pick'}
    <Card>
      <div class="step-title"><Heading level={6} as="h2">{unnumbered(t('import.stepPick'))}</Heading></div>
      <div class="hint"><Text size="sm" tone="muted">{t('import.pickHint')}</Text></div>

      <div class="tiles">
        <RadioGroup
          variant="cards"
          min="220px"
          name="source-pick"
          label={unnumbered(t('import.stepPick'))}
          value={source}
          options={sourceOptions}
          onValueChange={choose}
          class="bb-stagger"
          onpointerover={(e: PointerEvent) => hoverTile(e.target)}
          onpointerleave={() => (hover = null)}
        >
          {#snippet leading(option, on)}
            {@const s = IMPORT_STRATEGIES[option.value as ImportSource]}
            <span class="glyph" class:picked={on} aria-hidden="true">{s.initials}</span>
            {#if s.available}<Tag tone="pre">{t(CHIP_LABEL_KEYS[s.chip])}</Tag>{:else}<Tag tone="quiet">{t('import.chipSoon')}</Tag>{/if}
          {/snippet}
        </RadioGroup>
      </div>
      <div class="actions">
        <Button variant="ghost" type="button" onclick={() => onback(4)}>{t('onboarding.back')}</Button>
      </div>
    </Card>
  {:else if step === 'instructions' && source}
    {@const st = IMPORT_STRATEGIES[source]}
    {@const spec = st.input}
    <Card>
      <div class="instr-head">
        <span class="glyph" aria-hidden="true">{st.initials}</span>
        <Heading level={6} as="h2">{unnumbered(t('import.stepInstructions', { source: st.label }))}</Heading>
      </div>
      <div class="hint"><Text size="sm" tone="muted">{t('import.instrHint', { source: st.label })}</Text></div>

      {#if instrSteps.length}
        <ol class="steps">
          {#each instrSteps as s, i (i)}<Text as="li" size="sm">{s}</Text>{/each}
        </ol>
      {/if}

      {#if spec.kind === 'text'}
        {#if spec.linkHref && spec.linkLabel}
          <div class="instr-link">
            <Text size="sm"><TextLink variant="inline" href={spec.linkHref} external>{importText(spec.linkLabel)}</TextLink></Text>
          </div>
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
              aria-label={importText(spec.i18n.field)}
            />
          {:else}
            <Input
              fill
              mono
              placeholder={spec.placeholder}
              bind:value={session.credential}
              maxlength={spec.maxLen}
              spellcheck="false"
              autocomplete="off"
              autocapitalize="off"
              aria-label={importText(spec.i18n.field)}
            />
          {/if}
          {#if spec.i18n.hint}
            <div class="hint"><Text size="sm" tone="muted">{@html importText(spec.i18n.hint)}</Text></div>
          {/if}
        </div>
      {:else if spec.kind === 'oauth'}
        <div class="cred">
          {#if sourceConnected}
            <Tag tone="live" mark="solid" role="status">{importText(spec.i18n.connected)}</Tag>
          {:else}
            <ButtonLink href={`${spec.connectPath}?return=welcome`} variant="primary">
              {importText(spec.i18n.cta)}
            </ButtonLink>
          {/if}
          <div class="hint"><Text size="sm" tone="muted">{importText(spec.i18n.scopeHint)}</Text></div>
        </div>
      {:else}
        <FileDrop
          label={t('import.dropHint')}
          accept={spec.accept}
          file={uploadFile}
          onFileChange={pickFile}
          onDragChange={(over) => (hover = over ? 'surprised' : null)}
        />
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
          <Button variant="ghost" type="button" onclick={backFromInstructions}
            >{submitting ? t('common.cancel') : t('import.back')}</Button
          >
          <Button type="submit" variant="primary" busy={submitting}>
            {t('import.continueCta')}
          </Button>
        </div>
      </form>
    </Card>
  {:else if (step === 'commands' || step === 'extras' || step === 'review') && previewResult?.manifest}
    {#if step === 'review'}
    <Card>
      <div class="step-title"><Heading level={6} as="h2">{unnumbered(t('import.reviewTitle'))}</Heading></div>
      <div class="hint"><Text size="sm" tone="muted">{reviewHint}</Text></div>

      <div class="review-bar">
        {#each session.statChips as c (c)}<Tag tone="quiet">{c}</Tag>{/each}
        <span class="review-spacer"></span>
        <Button type="button" variant="ghost" size="sm" onclick={() => session.setAll(true)}>{t('import.selectAll')}</Button>
        <Button type="button" variant="ghost" size="sm" onclick={() => session.setAll(false)}>{t('import.selectNone')}</Button>
      </div>
    </Card>
    {:else}
      <div class="category-head">
        <Eyebrow>{step === 'commands' ? t('onboardingImport.commandsEyebrow') : t('onboardingImport.extrasEyebrow')}</Eyebrow>
        <div class="category-title"><Heading level={2}>{step === 'commands' ? t('onboardingImport.commandsTitle') : t('onboardingImport.extrasTitle')}</Heading></div>
        <Text tone="muted">{step === 'commands' ? t('onboardingImport.commandsBody') : t('onboardingImport.extrasBody')}</Text>
      </div>
      {#if step === 'extras'}
        <AlertBanner tone="warning" role="note" variant="callout"><b>{t('onboardingImport.modulesTitle')}</b>{t('onboardingImport.modulesBody')}</AlertBanner>
      {/if}
    {/if}

      {#each session.manifestLevelDiags as d (d.code + d.message)}
        <AlertBanner tone="warning" role="status">{d.message}</AlertBanner>
      {/each}

      {#if session.anyCollisions}
        <AlertBanner tone="danger" role="note" stack>
          <span class="conflict-lines">{@html t('import.conflictsNote', { n: previewResult.collisions?.length ?? 0 })}</span>
          {#snippet actions()}
            <span class="overwrite-toggle">
              <Checkbox bind:checked={session.overwrite} name="overwrite" value="on">{t('import.overwriteToggle')}</Checkbox>
            </span>
          {/snippet}
        </AlertBanner>
      {/if}

      {#if step === 'commands' && previewResult.manifest.commands?.length}
        <ImportGroup title={t('import.hCommands')} count={previewResult.manifest.commands.length}>
          <ul class="rows">
            {#each previewResult.manifest.commands as c, i (c.name)}
              <ImportRow {session} text={rowText} kind="commands" index={i} label={`!${c.name}`} collided={session.collidedCommands.has(normalizeName(c.name))}>
                {#snippet body()}
                  <Text as="span" size="sm" tone="muted">{c.responses?.join(' / ')}</Text>
                {/snippet}
                {#snippet chips()}
                  {#if c.permission && c.permission !== 'everyone'}<PermBadge perm={c.permission} />{/if}
                  {#if c.cooldown_seconds}<Tag tone="bare">{t('import.cooldownChip', { n: c.cooldown_seconds })}</Tag>{/if}
                  {#if c.user_cooldown_seconds}<Tag tone="bare">{t('import.userCooldownChip', { n: c.user_cooldown_seconds })}</Tag>{/if}
                  {#each c.aliases ?? [] as a (a)}<Tag bare literal>!{a}</Tag>{/each}
                {/snippet}
              </ImportRow>
            {/each}
          </ul>
        </ImportGroup>
      {/if}

      {#if step === 'extras' && previewResult.manifest.timers?.length}
        <ImportGroup title={t('import.hTimers')} count={previewResult.manifest.timers.length}>
          <ul class="rows">
            {#each previewResult.manifest.timers as tm, i (tm.message)}
              <ImportRow {session} text={rowText} kind="timers" index={i} ariaLabel={tm.message}>
                {#snippet body()}
                  <Text as="span" size="sm" tone="muted">{tm.message}</Text>
                {/snippet}
                {#snippet chips()}
                  <ImportTimerChips timer={tm} {locale} />
                {/snippet}
              </ImportRow>
            {/each}
          </ul>
        </ImportGroup>
      {/if}

      {#if step === 'extras' && previewResult.manifest.triggers?.length}
        <ImportGroup title={t('import.hTriggers')} count={previewResult.manifest.triggers.length}>
          <ul class="rows">
            {#each previewResult.manifest.triggers as tg, i (tg.phrase)}
              <ImportRow {session} text={rowText} kind="triggers" index={i} label={tg.phrase}>
                {#snippet body()}
                  <Text as="span" size="sm" tone="muted">{tg.response}</Text>
                {/snippet}
              </ImportRow>
            {/each}
          </ul>
        </ImportGroup>
      {/if}

      {#if step === 'extras' && previewResult.manifest.quotes?.length}
        <ImportGroup title={t('import.hQuotes')} count={previewResult.manifest.quotes.length}>
          <div class="group-note"><Text size="sm" tone="muted">{t('import.quotesAll', { n: previewResult.manifest.quotes.length })}</Text></div>
        </ImportGroup>
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
        <Label mono as="span">{session.selectionLine}</Label>
        <div class="actions-row">
          <Button variant="ghost" type="button" onclick={backFromReview}
            >{submitting ? t('common.cancel') : t('import.startOver')}</Button
          >
          <Button type="submit" variant="primary" busy={submitting}>
            {t('import.importNow')}
          </Button>
        </div>
      </form>
      {:else}
        <div class="category-actions">
          <Button variant="ghost" type="button" onclick={() => goStep(prevStep)}>{t('import.back')}</Button>
          <Button variant="primary" type="button" onclick={() => goStep(nextStep)}>{t('onboardingImport.continue')}</Button>
        </div>
      {/if}
  {:else if step === 'done'}
    <Card>
      <span class="done-seal" aria-hidden="true"><Icon name="check" size={22} /></span>
      <div class="step-title"><Heading level={6} as="h2">{unnumbered(t('import.doneTitle'))}</Heading></div>
      {#if commitResult}
        <div class="hint">
          <Text size="sm" tone="muted">
            {t('import.doneLine', {
              c: commitResult.applied.commands,
              tm: commitResult.applied.timers,
              tg: commitResult.applied.triggers,
              q: commitResult.applied.quotes
            })}
          </Text>
        </div>
        <ImportSkipped skipped={commitResult.skipped} {locale} />
        <ImportFailed
          failed={commitResult.failed}
          retryCount={session.retryCount}
          submitting={session.submitting}
          onRetry={() => session.runRetry()}
          {locale}
        />
        {#if commitError}<ImportAlert message={commitError} />{/if}
        {#if session.appliedTiles.length}
          <div class="applied bb-stagger">
            {#each session.appliedTiles as a (a.label)}
              <div class="applied-tile"><StatTile inline static label={a.label} value={String(a.n)} /></div>
            {/each}
          </div>
        {/if}
        {#each commitResult.diagnostics ?? [] as d (d.code + d.message)}
          <AlertBanner tone={d.severity === 'error' ? 'danger' : 'warning'} role="status">{d.message}</AlertBanner>
        {/each}
      {:else}
        <div class="hint"><Text size="sm" tone="muted">{t('import.nothingApplied')}</Text></div>
      {/if}
      <div class="actions actions-row done-actions">
        <Button onclick={() => finishOnboarding('/')} busy={finishing} tone="success">{t('onboardingImport.dashboard')}</Button>
        <Button variant="ghost" onclick={() => finishOnboarding('/commands')} disabled={finishing}>{t('import.reviewCommands')}</Button>
        <Button variant="ghost" onclick={reset} disabled={finishing}>{t('import.importAnother')}</Button>
      </div>
      {#if finishError}<ImportAlert message={finishError} />{/if}
      {#if commitResult?.audit_id}
        <div class="audit"><Text size="xs" mono tone="muted">{t('import.auditFoot', { n: commitResult.audit_id })}</Text></div>
      {/if}
    </Card>
  {/if}
  </div>
  {/key}
    </div>
</SetupStage>
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
  :global(.bb-setup-stage.welcome-import .bb-setup-stage__main) { grid-template-rows: auto minmax(0, 1fr); }
  :global(.bb-setup-stage.welcome-import .bb-setup-stage__content) { min-height: 0; }
  .category-head {
    padding: 26px 30px;
    border-radius: var(--bb-radius-md);
    background: var(--bb-elev-1);
    box-shadow: var(--bb-depth-1);
  }
  .category-title { margin-bottom: 12px; }
  .category-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    padding-top: 8px;
  }

  .step-title { margin-bottom: 6px; }
  .hint { margin: 0 0 12px; }

  .flow {
    min-width: 0;
    min-height: 0;
    display: grid;
    align-content: start;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-gutter: stable;
  }
  .scene {
    grid-area: 1 / 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
  .tiles,
  .applied {
    --stagger-duration: 640ms;
    --stagger-step: 70ms;
    --stagger-max: 1s;
    --bb-reveal-side: 1;
    --bb-reveal-shift: 22px;
  }
  .tiles {
    --stagger-delay: 320ms;
    --choice-min-h: 176px;
    margin: 16px 0 4px;
  }
  .glyph {
    flex: none;
    width: 34px;
    height: 34px;
    border-radius: var(--bb-radius-sm);
    background: var(--bb-elev-2);
    box-shadow: var(--bb-depth-0);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 12px;
    letter-spacing: 0.02em;
    color: var(--bb-tan-light);
    transition:
      background-color var(--bb-dur-fast) ease,
      color var(--bb-dur-fast) ease;
  }
  .glyph.picked {
    background: var(--bb-white);
    color: var(--bb-black);
    box-shadow: var(--bb-depth-1);
  }

  .steps {
    margin: 6px 0 16px;
    padding-left: 22px;
    display: flex;
    flex-direction: column;
    gap: 9px;
    overflow-wrap: anywhere;
  }
  .instr-link { margin: 0 0 14px; }
  .cred {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
    margin-bottom: 6px;
  }

  .done-actions { flex-wrap: wrap; min-height: 44px; }
  .actions {
    margin-top: 20px;
  }
  .actions-row {
    display: flex;
    gap: 10px;
    justify-content: flex-end;
  }

  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .conflict-lines { display: grid; gap: 10px; }
  .overwrite-toggle {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    --bb-check-align: center;
    white-space: nowrap;
  }

  .instr-head {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 14px;
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

  .group-note {
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
    border-radius: var(--bb-radius-pill);
    background: var(--bb-elev-2);
    padding: 14px 16px 14px 24px;
    box-shadow: var(--bb-depth-3);
  }

  .done-seal {
    display: inline-grid;
    place-items: center;
    width: 52px;
    height: 52px;
    margin-bottom: 14px;
    border-radius: 50%;
    background: rgba(var(--bb-green-glow-rgb), .14);
    color: var(--bb-green-glow);
    box-shadow: 0 0 0 6px rgba(var(--bb-green-glow-rgb), .06);
    animation: seal-in var(--bb-dur-slow) var(--bb-ease-out-expo) 360ms both;
  }
  .applied {
    --stagger-delay: 520ms;
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
    gap: 12px;
    margin: 22px 0 26px;
  }
  .applied-tile {
    padding: 16px 18px;
    border-radius: var(--bb-radius-md);
    background: var(--bb-elev-2);
    box-shadow: var(--bb-depth-1);
  }
  .audit { margin-top: 24px; }

  @media (max-width: 760px), (max-height: 760px) {
    .tiles { --choice-min-h: 0; margin: 8px 0 0; }
    .tiles :global(.bb-choice__desc) { display: none; }
    .scene :global(.bb-card) { --card-pad: 16px; }
    .scene { gap: 12px; }
    .actions { margin-top: 12px; }
  }
  @media (max-width: 560px) {
    .tiles :global(.bb-choices) { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .commit-bar {
      border-radius: var(--bb-radius-sm);
      padding: 14px 16px;
    }
  }

  @keyframes seal-in {
    from { opacity: 0; transform: scale(.4); }
    to { opacity: 1; transform: none; }
  }

  @media (prefers-reduced-motion: reduce) {
    .done-seal { animation: none; }
    .glyph { transition: none; }
  }
</style>
