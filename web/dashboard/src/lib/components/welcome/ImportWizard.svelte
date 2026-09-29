<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { fly } from 'svelte/transition';
  import { page } from '$app/state';
  import { translate, translateList, type Locale } from '@bagel/kit/i18n';
  import { bezier } from '@bagel/ui/lib/tween';
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import Sky from '@bagel/ui/svelte/Sky.svelte';
  import Completion from '$lib/components/welcome/Completion.svelte';
  import { deserialize } from '$app/forms';
  import {
    AlertBanner,
    PermBadge,
    Bolota,
    Button,
    ButtonLink,
    Card,
    Checkbox,
    Eyebrow,
    FileDrop,
    Heading,
    Icon,
    Input,
    Label,
    RadioGroup,
    StatTile,
    Stepper,
    Tag,
    Text,
    TextLink,
    Textarea,
    parallax,
    toast
  } from '@bagel/kit';
  import { applyImportCaps } from '@bagel/kit/importer/caps';
  import {
    CHIP_LABEL_KEYS,
    IMPORT_STRATEGIES,
    isImportSource,
    type FileInputSpec,
    type ImportSourceStrategy,
    type InputSpec,
    type OAuthInputSpec,
    type TextInputSpec
  } from '@bagel/kit/importer/strategy';
  import {
    IMPORT_SOURCES,
    type CommitResponse,
    type ImportDiagnostic,
    type ImportSource,
    type ManifestCommand,
    type ManifestQuote,
    type ManifestTimer,
    type ManifestTrigger,
    type PreviewResponse
  } from '@bagel/kit';

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
  // svelte-ignore state_referenced_locally
  let source = $state<ImportSource | ''>(deepLinkSource());
  // svelte-ignore state_referenced_locally
  let step = $state<Step>(source ? 'instructions' : 'pick');

  function deepLinkSource(): ImportSource | '' {
    const q = page.url.searchParams.get('source') ?? '';
    if (!isImportSource(q)) return '';
    return IMPORT_STRATEGIES[q].available ? q : '';
  }

  const strategy = $derived<ImportSourceStrategy | null>(source ? IMPORT_STRATEGIES[source] : null);
  const inputSpec = $derived<InputSpec | null>(strategy?.input ?? null);

  const connected = $derived((page.data.connected ?? {}) as Partial<Record<ImportSource, boolean>>);
  const sourceConnected = $derived(source ? connected[source] === true : false);
  let credential = $state('');
  let uploadFile = $state<File | null>(null);
  let submitting = $state(false);

  let previewResult = $state<PreviewResponse | null>(null);
  let commitResult = $state<CommitResponse | null>(null);
  let finishError = $state('');
  let finishing = $state(false);

  const ORDER: Step[] = ['pick', 'instructions', 'commands', 'extras', 'review', 'done'];

  const unnumbered = (s: string) => s.replace(/^\s*\d+\s*·\s*/, '');
  const stepIndex = $derived(ORDER.indexOf(step));
  const journeyStep = $derived(BEFORE_IMPORT.length + stepIndex);
  const journeySteps = $derived([...BEFORE_IMPORT, ...STAGES].map((label) => ({ label })));
  const maxRailStep = $derived(stepIndex);
  function selectRail(i: number) {
    if (i < BEFORE_IMPORT.length) {
      onback(i + 1);
      return;
    }
    const importIndex = i - BEFORE_IMPORT.length;
    const target = ORDER[importIndex];
    if (importIndex <= maxRailStep && (importIndex < 2 || previewResult || target === 'done' && commitResult)) goStep(target);
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
    const forward = ORDER.indexOf(target) > stepIndex;
    dir = forward ? 1 : -1;
    hover = null;
    step = target;
    play('entrance');
  }

  onMount(() => {
    return () => {
      if (reactionTimer) clearTimeout(reactionTimer);
    };
  });

  let px = $state(0);
  let py = $state(0);
  function follow(point: { px: number; py: number }) {
    px = point.px;
    py = point.py;
  }

  const sourceOptions = $derived(
    IMPORT_SOURCES.map((id) => {
      const s = IMPORT_STRATEGIES[id];
      return {
        value: id,
        label: s.label,
        description: t(s.i18n.desc),
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
    source = value;
    uploadFile = null;
    credential = '';
    react('excited', 1400);
    goStep('instructions');
  }

  function reset() {
    goStep('pick');
    source = '';
    uploadFile = null;
    credential = '';
    overwrite = false;
    previewResult = null;
    commitResult = null;
    previewError = '';
    commitError = '';
  }

  type RowKind = 'commands' | 'timers' | 'triggers' | 'quotes';

  let selected = $state<Record<string, boolean>>({});
  let overwrite = $state(false);

  const CODE_PREFIX: Record<RowKind, string> = {
    commands: 'command',
    timers: 'timer',
    triggers: 'trigger',
    quotes: 'quote'
  };

  function itemDiags(kind: RowKind, i: number): ImportDiagnostic[] {
    return (previewResult?.diagnostics ?? []).filter(
      (d) => d.item_index === i && d.code.startsWith(CODE_PREFIX[kind])
    );
  }

  const fatalCount = $derived.by(() => {
    const m = previewResult?.manifest;
    if (!m) return 0;
    let n = 0;
    for (const kind of Object.keys(CODE_PREFIX) as RowKind[]) {
      const rows = (m[kind] as unknown[] | undefined) ?? [];
      for (let i = 0; i < rows.length; i++)
        if (itemDiags(kind, i).some((d) => d.severity === 'error')) n++;
    }
    return n;
  });

  function initializeSelection() {
    const m = previewResult?.manifest;
    if (!m) return;
    const next: Record<string, boolean> = {};
    for (const kind of Object.keys(CODE_PREFIX) as RowKind[]) {
      const rows = (m[kind] as unknown[] | undefined) ?? [];
      for (let i = 0; i < rows.length; i++) {
        next[`${kind}:${i}`] = !itemDiags(kind, i).some((d) => d.severity === 'error');
      }
    }
    selected = next;
  }

  function isChecked(kind: RowKind, i: number): boolean {
    return selected[`${kind}:${i}`] !== false;
  }

  function toggle(kind: RowKind, i: number, checked: boolean) {
    selected = { ...selected, [`${kind}:${i}`]: checked };
  }

  function normalizeName(n: string): string {
    return n.trim().replace(/^!/, '').trim().toLowerCase();
  }
  const collidedCommands = $derived(
    new Set(
      (previewResult?.collisions ?? [])
        .filter((c) => c.kind === 'command')
        .map((c) => c.name)
    )
  );
  const anyCollisions = $derived((previewResult?.collisions ?? []).length > 0);

  const statChips = $derived.by(() => {
    const s = previewResult?.stats;
    if (!s) return [] as string[];
    const parts: string[] = [];
    if (s.commands) parts.push(t('import.statCommands', { n: s.commands }));
    if (s.timers) parts.push(t('import.statTimers', { n: s.timers }));
    if (s.triggers) parts.push(t('import.statTriggers', { n: s.triggers }));
    if (s.quotes) parts.push(t('import.statQuotes', { n: s.quotes }));
    return parts;
  });

  const statsLine = $derived(statChips.join(' · ') || t('import.statsNone'));

  function setAll(v: boolean) {
    const m = previewResult?.manifest;
    if (!m) return;
    const next: Record<string, boolean> = {};
    for (const kind of Object.keys(CODE_PREFIX) as RowKind[]) {
      const rows = (m[kind] as unknown[] | undefined) ?? [];
      for (let i = 0; i < rows.length; i++) {
        next[`${kind}:${i}`] = v && !itemDiags(kind, i).some((d) => d.severity === 'error');
      }
    }
    selected = next;
  }

  const rowTotal = $derived.by(() => {
    const m = previewResult?.manifest;
    if (!m) return 0;
    return (Object.keys(CODE_PREFIX) as RowKind[]).reduce(
      (n, kind) => n + ((m[kind] as unknown[] | undefined) ?? []).length,
      0
    );
  });
  const rowPicked = $derived.by(() => {
    const m = previewResult?.manifest;
    if (!m) return 0;
    let n = 0;
    for (const kind of Object.keys(CODE_PREFIX) as RowKind[]) {
      const rows = (m[kind] as unknown[] | undefined) ?? [];
      for (let i = 0; i < rows.length; i++) if (isChecked(kind, i)) n++;
    }
    return n;
  });
  const selectionLine = $derived(t('import.selectionLine', { n: rowPicked, total: rowTotal }));

  const railDetail = $derived.by(() => [
    strategy ? strategy.label : t('import.railPickPending'),
    inputDetail(),
    previewResult ? t('onboardingImport.commandsCount', { n: previewResult.manifest?.commands?.length ?? 0 }) : '',
    previewResult ? statsLine : '',
    previewResult ? selectionLine : '',
    commitResult ? t('import.railDone') : ''
  ]);

  function inputDetail(): string {
    if (inputSpec?.kind === 'text') return textInputDetail(inputSpec);
    return uploadFile ? uploadFile.name : t('import.railFilePending');
  }

  function textInputDetail(spec: TextInputSpec): string {
    if (spec.secret) return credential ? t('import.railTokenSet') : t('import.railTokenPending');
    return credential ? t('import.railHandleSet') : t('import.railHandlePending');
  }

  const appliedTiles = $derived.by(() => {
    const a = commitResult?.applied;
    if (!a) return [] as { n: number; label: string }[];
    const out: { n: number; label: string }[] = [];
    if (a.commands) out.push({ n: a.commands, label: t('import.hCommands') });
    if (a.timers) out.push({ n: a.timers, label: t('import.hTimers') });
    if (a.triggers) out.push({ n: a.triggers, label: t('import.hTriggers') });
    if (a.quotes) out.push({ n: a.quotes, label: t('import.hQuotes') });
    return out;
  });

  const reviewHint = $derived.by(() => {
    if (!strategy) return '';
    let s = t('import.reviewHint', { source: strategy.label, stats: statsLine });
    if (fatalCount > 0) s += ' ' + t('import.fatalSuffix', { n: fatalCount });
    return s;
  });

  const manifestLevelDiags = $derived(
    (previewResult?.diagnostics ?? []).filter((d) => d.item_index < 0)
  );

  function buildSelectedManifest(): string {
    const m = previewResult?.manifest;
    if (!m) return '{}';
    const out: Record<string, unknown> = {};
    const keep = (rows: unknown[] | undefined, kind: RowKind): unknown[] | undefined => {
      const picked = (rows ?? []).filter((_, i) => isChecked(kind, i));
      return picked.length ? picked : undefined;
    };
    out.commands = keep(m.commands as ManifestCommand[] | undefined, 'commands');
    out.timers = keep(m.timers as ManifestTimer[] | undefined, 'timers');
    out.triggers = keep(m.triggers as ManifestTrigger[] | undefined, 'triggers');
    out.quotes = keep(m.quotes as ManifestQuote[] | undefined, 'quotes');
    if (m.automod && rowPicked > 0) out.automod = m.automod;
    return JSON.stringify(out);
  }

  const instrSteps = $derived.by(() => {
    const key = strategy?.i18n.instr;
    return key ? tl(key) : [];
  });

  // svelte-ignore state_referenced_locally
  let previewError = $state(deepLinkError());

  function deepLinkError(): string {
    const e = page.url.searchParams.get('e');
    const spec = source ? IMPORT_STRATEGIES[source].input : null;
    if (!e || spec?.kind !== 'oauth') return '';
    const key = spec.errorParams[e];
    return key ? t(key) : '';
  }

  let commitError = $state('');

  async function runPreview() {
    if (!source || submitting) return;
    previewError = '';

    const body = new FormData();
    body.set('source', source);

    submitting = true;
    const prepared = await prepareInput(IMPORT_STRATEGIES[source].input, body);
    if ('error' in prepared) {
      previewError = prepared.error;
      react('attentive', 2400);
      submitting = false;
      return;
    }
    play('entrance');

    const r = await postPreview(body);
    if (r.ok && r.preview) {
      r.preview.diagnostics = [...prepared.diags, ...(r.preview.diagnostics ?? [])];
      previewResult = r.preview;
      initializeSelection();
      react('proud', 1800);
      goStep(r.preview.manifest?.commands?.length ? 'commands' : 'extras');
    } else {
      previewError = r.error || t('import.errGeneric');
      react('attentive', 2400);
    }
    submitting = false;
  }

  type PreparedInput = { error: string } | { diags: ImportDiagnostic[] };

  function prepareInput(spec: InputSpec, body: FormData): Promise<PreparedInput> | PreparedInput {
    if (spec.kind === 'text') return prepareText(spec, body);
    if (spec.kind === 'oauth') return prepareOauth(spec);
    return prepareFile(spec, body);
  }

  function prepareText(spec: TextInputSpec, body: FormData): PreparedInput {
    const value = credential.trim();
    if (value === '') return { error: t(spec.i18n.errMissing) };
    if (!acceptableText(spec, value)) return { error: t(spec.i18n.errShape) };
    body.set('credential', value);
    return { diags: [] };
  }

  function acceptableText(spec: TextInputSpec, value: string): boolean {
    return value.length <= spec.maxLen && spec.shape.test(value);
  }

  function prepareOauth(spec: OAuthInputSpec): PreparedInput {
    if (!sourceConnected) return { error: t(spec.i18n.errNotConnected) };
    return { diags: [] };
  }

  async function prepareFile(spec: FileInputSpec, body: FormData): Promise<PreparedInput> {
    const file = uploadFile;
    if (!file || file.size === 0) return { error: t('import.errFileMissing') };
    if (file.size > spec.maxBytes)
      return { error: t('import.errTooLarge', { limit: Math.round(spec.maxBytes / (1024 * 1024)) }) };
    if (!spec.parseInBrowser) {
      body.set('file', file);
      return { diags: [] };
    }
    return parseInPage(spec.parseInBrowser, file, body);
  }

  async function parseInPage(
    parse: NonNullable<FileInputSpec['parseInBrowser']>,
    file: File,
    body: FormData
  ): Promise<PreparedInput> {
    try {
      const parsed = parse(new Uint8Array(await file.arrayBuffer()));
      const capped = applyImportCaps(parsed.manifest);
      body.set('manifest', JSON.stringify(capped.manifest));
      return { diags: [...capped.diagnostics] };
    } catch (err) {
      return { error: parseErrorMessage(err) };
    }
  }

  function parseErrorMessage(err: unknown): string {
    const message = err instanceof Error ? err.message : '';
    const parserRefusal = /^importer\/[a-z-]+:\s*([\s\S]*)$/.exec(message);
    if (!parserRefusal) return t('import.errGeneric');
    return t('import.errParseFailed', { m: parserRefusal[1] });
  }

  async function postPreview(body: FormData): Promise<{ ok: boolean; preview?: PreviewResponse; error?: string }> {
    try {
      const res = await fetch('/settings/import?/preview', { method: 'POST', body });
      const r = deserialize(await res.text());
      if (r.type === 'failure') {
        const d = r.data as { error?: string } | undefined;
        return { ok: false, error: d?.error };
      }
      if (r.type === 'success') {
        const d = r.data as { ok?: boolean; preview?: PreviewResponse } | undefined;
        if (d?.ok && d.preview?.manifest) return { ok: true, preview: d.preview };
      }
      return { ok: false };
    } catch {
      return { ok: false };
    }
  }

  let showCompletion = $state(false);
  let finishSaved: Promise<boolean> = Promise.resolve(false);

  function finishOnboarding() {
    if (finishing) return;
    finishing = true;
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
      finishFailed(
        result.type === 'failure'
          ? (result.data as { error?: string } | undefined)?.error || t('onboardingImport.saveFailed')
          : t('onboardingImport.saveFailed')
      );
    } catch {
      finishFailed(t('onboardingImport.saveFailed'));
    }
    return false;
  }

  function finishFailed(message: string) {
    finishError = message;
    showCompletion = false;
    finishing = false;
    react('attentive', 2400);
  }

  async function afterCompletion() {
    if (await finishSaved) onexit('/');
  }

  async function runCommit() {
    if (submitting) return;
    commitError = '';
    const manifestJson = buildSelectedManifest();
    if (manifestJson === '{}') {
      commitError = t('import.errNothingSelected');
      return;
    }

    submitting = true;
    const body = new FormData();
    body.set('manifest', manifestJson);
    body.set('source', source);
    if (overwrite) body.set('overwrite', 'on');

    try {
      const res = await fetch('/settings/import?/commit', { method: 'POST', body });
      const r = deserialize(await res.text());
      if (r.type === 'failure') {
        const d = r.data as { error?: string } | undefined;
        commitError = d?.error || t('import.errGeneric');
      } else if (r.type === 'success') {
        const d = r.data as { ok?: boolean; commit?: CommitResponse } | undefined;
        if (d?.ok && d.commit) {
          commitResult = d.commit;
          react('love', 2200);
          goStep('done');
          play('burst');
          toast('ok', t('import.toastApplied'));
        } else {
          commitError = t('import.errGeneric');
        }
      } else {
        commitError = t('import.errGeneric');
      }
    } catch {
      commitError = t('import.errGeneric');
    }
    submitting = false;
  }

  function pickFile(f: File | null | undefined) {
    previewError = '';
    if (!f) {
      uploadFile = null;
      return;
    }
    const want = wantedExtension();
    if (want && !f.name.toLowerCase().endsWith(want)) {
      previewError = t('import.errWrongType', { want });
      uploadFile = null;
      react('attentive', 2400);
      return;
    }
    uploadFile = f;
    react('happy', 1600);
  }

  function wantedExtension(): string {
    if (inputSpec?.kind !== 'file') return '';
    const first = inputSpec.accept.split(',')[0];
    return first.startsWith('.') ? first : '';
  }
</script>

<svelte:head><title>{t('onboardingImport.pageTitle')} · ItsBagelBot</title><meta name="robots" content="noindex, nofollow" /></svelte:head>
<Sky
  shift={stepIndex % 2 ? 1 : -1}
  turn={stepIndex * 24}
  {px}
  {py}
  progress={journeyStep / (journeySteps.length - 1)}
  leaving={showCompletion}
/>
<div class="welcome-import" class:leaving={showCompletion} data-orbs="off" use:parallax={{ scope: 'viewport', onmove: follow }}>
  <header class="top">
    <Brand title="ItsBagelBot" sub={t('common.console')} logoSrc="/logo.png" logoAlt="" size="md" />
    <Stepper steps={journeySteps} current={journeyStep} maxStep={journeyStep} label={t('onboarding.stepOf', { n: journeyStep + 1, total: journeySteps.length })} onselect={selectRail} />
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
    <div class="flow">
  {#key step}
  <div class="scene" in:sceneIn out:sceneOut>

  {#if step === 'pick'}
    <Card glass>
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
          onchange={choose}
          class="bb-stagger"
          onpointerover={(e: PointerEvent) => hoverTile(e.target)}
          onpointerleave={() => (hover = null)}
        >
          {#snippet lead(option, on)}
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
    <Card glass>
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
            <Text size="sm"><TextLink variant="inline" href={spec.linkHref} external>{t(spec.linkLabel)}</TextLink></Text>
          </div>
        {/if}
        <div class="cred">
          {#if spec.secret}
            <Textarea
              rows={3}
              fill
              mono
              placeholder={spec.placeholder}
              bind:value={credential}
              spellcheck="false"
              autocomplete="off"
              autocapitalize="off"
              aria-label={t(spec.i18n.field)}
            />
          {:else}
            <Input
              fill
              mono
              placeholder={spec.placeholder}
              bind:value={credential}
              maxlength={spec.maxLen}
              spellcheck="false"
              autocomplete="off"
              autocapitalize="off"
              aria-label={t(spec.i18n.field)}
            />
          {/if}
          {#if spec.i18n.hint}
            <div class="hint"><Text size="sm" tone="muted">{@html t(spec.i18n.hint)}</Text></div>
          {/if}
        </div>
      {:else if spec.kind === 'oauth'}
        <div class="cred">
          {#if sourceConnected}
            <Tag tone="live" mark="solid" role="status">{t(spec.i18n.connected)}</Tag>
          {:else}
            <ButtonLink href={`${spec.connectPath}?return=welcome`} variant="primary">
              {t(spec.i18n.cta)}
            </ButtonLink>
          {/if}
          <div class="hint"><Text size="sm" tone="muted">{t(spec.i18n.scopeHint)}</Text></div>
        </div>
      {:else}
        <FileDrop
          label={t('import.dropHint')}
          accept={spec.accept}
          file={uploadFile}
          onfile={pickFile}
          ondragchange={(over) => (hover = over ? 'surprised' : null)}
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
          <Button variant="ghost" type="button" onclick={() => goStep('pick')} disabled={submitting}
            >{t('import.back')}</Button
          >
          <Button type="submit" variant="primary" loading={submitting}>
            {t('import.continueCta')}
          </Button>
        </div>
      </form>
    </Card>
  {:else if (step === 'commands' || step === 'extras' || step === 'review') && previewResult?.manifest}
    {#if step === 'review'}
    <Card glass>
      <div class="step-title"><Heading level={6} as="h2">{unnumbered(t('import.reviewTitle'))}</Heading></div>
      <div class="hint"><Text size="sm" tone="muted">{reviewHint}</Text></div>

      <div class="review-bar">
        {#each statChips as c (c)}<Tag tone="quiet">{c}</Tag>{/each}
        <span class="review-spacer"></span>
        <Button type="button" variant="ghost" size="sm" onclick={() => setAll(true)}>{t('import.selectAll')}</Button>
        <Button type="button" variant="ghost" size="sm" onclick={() => setAll(false)}>{t('import.selectNone')}</Button>
      </div>
    </Card>
    {:else}
      <div class="category-head">
        <Eyebrow>{step === 'commands' ? t('onboardingImport.commandsEyebrow') : t('onboardingImport.extrasEyebrow')}</Eyebrow>
        <div class="category-title"><Heading level={2}>{step === 'commands' ? t('onboardingImport.commandsTitle') : t('onboardingImport.extrasTitle')}</Heading></div>
        <Text tone="muted">{step === 'commands' ? t('onboardingImport.commandsBody') : t('onboardingImport.extrasBody')}</Text>
      </div>
      {#if step === 'extras'}
        <AlertBanner variant="warn" role="note" callout><b>{t('onboardingImport.modulesTitle')}</b>{t('onboardingImport.modulesBody')}</AlertBanner>
      {/if}
    {/if}

      {#each manifestLevelDiags as d (d.code + d.message)}
        <AlertBanner variant="warn" role="status">{d.message}</AlertBanner>
      {/each}

      {#if anyCollisions}
        <AlertBanner variant="danger" role="note" stack>
          <span class="conflict-lines">{@html t('import.conflictsNote', { n: previewResult.collisions?.length ?? 0 })}</span>
          {#snippet action()}
            <span class="overwrite-toggle">
              <Checkbox bind:checked={overwrite} name="overwrite" value="on">{t('import.overwriteToggle')}</Checkbox>
            </span>
          {/snippet}
        </AlertBanner>
      {/if}

      {#if step === 'commands' && previewResult.manifest.commands?.length}
        <Card glass flush>
          <div class="group-head">
            <Eyebrow>{t('import.hCommands')}</Eyebrow>
            <Text as="span" size="xs" mono tone="muted">{previewResult.manifest.commands.length}</Text>
          </div>
          <ul class="rows">
            {#each previewResult.manifest.commands as c, i (c.name)}
              {@const diags = itemDiags('commands', i)}
              <li class="row-item" class:collision={collidedCommands.has(normalizeName(c.name))}>
                <span class="pick">
                  <Checkbox bind:checked={() => isChecked('commands', i), (on) => toggle('commands', i, on)}><Text as="span" size="sm" mono>!{c.name}</Text></Checkbox>
                </span>
                <div class="row-body">
                  <Text as="span" size="sm" tone="muted">{c.responses?.join(' / ')}</Text>
                  <span class="chips">
                    {#if c.permission && c.permission !== 'everyone'}<PermBadge perm={c.permission} />{/if}
                    {#if c.cooldown_seconds}<Tag tone="bare">{t('import.cooldownChip', { n: c.cooldown_seconds })}</Tag>{/if}
                    {#each c.aliases ?? [] as a (a)}<Tag bare literal>!{a}</Tag>{/each}
                    {#each diags.filter((d) => d.severity === 'warn') as d (d.code + d.message)}
                      <Tag tone="alpha" title={d.message}>{d.message}</Tag>
                    {/each}
                    {#each diags.filter((d) => d.severity === 'error') as d (d.code + d.message)}
                      <Tag tone="error" title={d.message}>{t('import.cannotImport', { m: d.message })}</Tag>
                    {/each}
                    {#if collidedCommands.has(normalizeName(c.name))}
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
        <Card glass flush>
          <div class="group-head">
            <Eyebrow>{t('import.hTimers')}</Eyebrow>
            <Text as="span" size="xs" mono tone="muted">{previewResult.manifest.timers.length}</Text>
          </div>
          <ul class="rows">
            {#each previewResult.manifest.timers as tm, i (tm.message)}
              {@const diags = itemDiags('timers', i)}
              <li class="row-item">
                <span class="pick">
                  <Checkbox bind:checked={() => isChecked('timers', i), (on) => toggle('timers', i, on)} aria-label={tm.message} />
                </span>
                <div class="row-body">
                  <Text as="span" size="sm" tone="muted">{tm.message}</Text>
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
        <Card glass flush>
          <div class="group-head">
            <Eyebrow>{t('import.hTriggers')}</Eyebrow>
            <Text as="span" size="xs" mono tone="muted">{previewResult.manifest.triggers.length}</Text>
          </div>
          <ul class="rows">
            {#each previewResult.manifest.triggers as tg, i (tg.phrase)}
              {@const diags = itemDiags('triggers', i)}
              <li class="row-item">
                <span class="pick">
                  <Checkbox bind:checked={() => isChecked('triggers', i), (on) => toggle('triggers', i, on)}><Text as="span" size="sm" mono>{tg.phrase}</Text></Checkbox>
                </span>
                <div class="row-body">
                  <Text as="span" size="sm" tone="muted">{tg.response}</Text>
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
        <Card glass flush>
          <div class="group-head">
            <Eyebrow>{t('import.hQuotes')}</Eyebrow>
            <Text as="span" size="xs" mono tone="muted">{previewResult.manifest.quotes.length}</Text>
          </div>
          <div class="group-note"><Text size="sm" tone="muted">{t('import.quotesAll', { n: previewResult.manifest.quotes.length })}</Text></div>
        </Card>
      {/if}

      {#if commitError}<AlertBanner>{commitError}</AlertBanner>{/if}

      {#if step === 'review'}
      <form
        class="commit-bar"
        onsubmit={(e) => {
          e.preventDefault();
          runCommit();
        }}
      >
        <Label mono as="span">{selectionLine}</Label>
        <div class="actions-row">
          <Button variant="ghost" type="button" onclick={reset} disabled={submitting}
            >{t('import.startOver')}</Button
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
    <Card glass>
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
            {#if commitResult.skipped?.length}
              {t('import.skippedLine', {
                n: commitResult.skipped.length,
                names: commitResult.skipped.map((c) => c.name).join(', ')
              })}
            {/if}
          </Text>
        </div>
        {#if appliedTiles.length}
          <div class="applied bb-stagger">
            {#each appliedTiles as a (a.label)}
              <div class="applied-tile"><StatTile inline static label={a.label} value={String(a.n)} /></div>
            {/each}
          </div>
        {/if}
        {#each commitResult.diagnostics ?? [] as d (d.code + d.message)}
          <AlertBanner variant={d.severity === 'error' ? 'danger' : 'warn'} role="status">{d.message}</AlertBanner>
        {/each}
      {:else}
        <div class="hint"><Text size="sm" tone="muted">{t('import.nothingApplied')}</Text></div>
      {/if}
      <div class="actions">
        <Button variant="primary" onclick={finishOnboarding} loading={finishing}>{t('onboardingImport.dashboard')}</Button>
      </div>
      {#if finishError}<AlertBanner>{finishError}</AlertBanner>{/if}
      {#if commitResult?.audit_id}
        <div class="audit"><Text size="xs" mono tone="muted">{t('import.auditFoot', { n: commitResult.audit_id })}</Text></div>
      {/if}
    </Card>
  {/if}
  </div>
  {/key}
    </div>
  </div>
</section>
</div>
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
  .top { animation: settle calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo) backwards; }
  .welcome-import .screen { animation: arrive-far var(--bb-dur-slow) var(--bb-ease-out-expo) backwards; }
  .leaving .top,
  .leaving .screen {
    opacity: 0;
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 24px;
    min-height: 96px;
  }
  .welcome-import .screen {
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
    filter: drop-shadow(0 14px 26px rgba(var(--bb-shadow-rgb), .38));
    transition: transform calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo);
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
    background: rgba(var(--bb-card-bg-rgb), .7);
    backdrop-filter: blur(14px);
    overflow: hidden;
    transition: transform calc(var(--bb-dur-slow) * 1.5) var(--bb-ease-out-expo);
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
  .category-head {
    padding: 26px 30px;
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-lg);
    background: rgba(var(--bb-card-bg-rgb), .76);
    backdrop-filter: blur(18px);
  }
  .category-title { margin-bottom: 12px; }
  .category-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    padding-top: 8px;
  }

  @media (max-width: 760px) {
    .top { flex-wrap: wrap; padding: 18px 0; }
    .welcome-import .screen { margin-top: 24px; }
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

  .step-title { margin-bottom: 6px; }
  .hint { margin: 0 0 12px; }

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
    border: 1px solid var(--bb-glass-border);
    background: rgba(var(--bb-white-pure-rgb), 0.04);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 12px;
    letter-spacing: 0.02em;
    color: var(--bb-tan-light);
    transition:
      border-color var(--bb-dur-fast) ease,
      color var(--bb-dur-fast) ease;
  }
  .glyph.picked {
    border-color: rgba(var(--bb-tan-rgb), 0.55);
    color: var(--bb-tan);
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
  .row-item {
    display: flex;
    align-items: baseline;
    gap: 14px;
    border: 1px solid var(--bb-glass-border);
    border-radius: var(--bb-radius-sm);
    padding: 16px 22px;
    background: var(--bb-glass-fill);
  }
  .row-item.collision {
    border-color: rgba(var(--bb-status-error-border-rgb), 0.55);
  }
  .pick {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    white-space: nowrap;
    --bb-check-align: center;
  }
  .row-body {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
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

  .group-head {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 16px 22px;
    border-bottom: 1px solid var(--bb-border);
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
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-pill);
    background: rgba(var(--bb-card-bg-rgb), 0.92);
    backdrop-filter: blur(18px);
    padding: 14px 16px 14px 24px;
    box-shadow: 0 8px 30px rgba(var(--bb-shadow-rgb), 0.35);
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
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-md);
    background: var(--bb-glass-fill);
  }
  .audit { margin-top: 24px; }

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
    .welcome-import .screen,
    .companion-scale,
    .companion-plate,
    .done-seal { animation: none; }
    .companion-blob,
    .bubble,
    .glyph { transition: none; }
  }
</style>
