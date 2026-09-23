<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // The onboarding branch of the configuration importer. Source selection,
  // connection, commands, other imported items and final review are separate
  // scenes. The underlying importer remains the settings importer: this route
  // posts to its preview and commit actions, so the validation and writes are
  // identical on both paths. ?source= resumes after an OAuth round trip.
  //
  // Actions are hit with fetch + devalue (`/settings/import?/…`) instead of
  // `use:enhance`: enhance funnels results into whatever `form` prop the
  // CURRENT page load has, and keeping the posts manual means the step state
  // machine below fully owns when review/done render, with no reload wiping it.
  // Wire shapes come from @bagel/kit, single source:
  // web/kit/lib/importer/types.ts since the importer service folded
  // into the dashboard.
  //
  // Every per-source difference lives in ONE place: the strategy registry in
  // shared/lib/importer/strategy.ts. This page renders tiles, an input block
  // and a preview post off the picked strategy and names no source anywhere,
  // so a new source is an entry there plus one server-side object, never a
  // branch here. Before the split (2026-09-07) this file carried five parallel
  // per-source tables and a `source === '…'` branch per input kind.
  //
  // Client-side parsing: a file source may carry parseInBrowser (Moobot does,
  // pinned against the Go parser it was ported from), in which case the export
  // is decoded HERE and only the resulting manifest is POSTed: raw files no
  // longer cross the wire for that source. StreamLabs .db stays a server-side
  // upload because console CSP forbids WASM (no wasm-unsafe-eval in
  // script-src), which rules out an in-browser SQLite reader; see the decision
  // record at prepareFile.

  //
  // Motion: the same lateral grammar as /welcome. Each stage is one scene in
  // a grid cell; moving forward slides the old scene out toward the start and
  // the new one in from the far side (reversed going back), on the site's one
  // curve. Bolota rides above the scenes as the constant: it reacts to what
  // is happening (a source picked, a file dropped, a preview landing) and
  // carries the stage name, so the wizard reads as the same companion walking
  // you through, not a form that swaps panels.
  import { onMount } from 'svelte';
  import { fly } from 'svelte/transition';
  import { page } from '$app/state';
  import { translate, translateList, type Locale } from '@bagel/kit/i18n';
  import { bezier } from '@bagel/ui/lib/tween';
  import { hasFinePointer, prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import Sky from '$lib/components/welcome/Sky.svelte';
  import StepRail from '$lib/components/welcome/StepRail.svelte';
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
    Heading,
    Icon,
    Tag,
    Textarea,
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

  // --- step state ----------------------------------------------------------
  type Step = 'pick' | 'instructions' | 'commands' | 'extras' | 'review' | 'done';
  // svelte-ignore state_referenced_locally
  let source = $state<ImportSource | ''>(deepLinkSource());
  // A ?source= deep link lands on that source's instructions directly: this
  // is what brings the wizard back mid-flow after an OAuth round trip instead
  // of dropping the user on the picker again.
  // svelte-ignore state_referenced_locally
  let step = $state<Step>(source ? 'instructions' : 'pick');

  function deepLinkSource(): ImportSource | '' {
    const q = page.url.searchParams.get('source') ?? '';
    if (!isImportSource(q)) return '';
    // A source with no working input has no instructions step to land on, so
    // the deep link falls back to the plain picker.
    return IMPORT_STRATEGIES[q].available ? q : '';
  }

  // The picked source's strategy. Everything the steps below render, validate
  // and post comes off it, which is what keeps this page from naming a source.
  const strategy = $derived<ImportSourceStrategy | null>(source ? IMPORT_STRATEGIES[source] : null);
  const inputSpec = $derived<InputSpec | null>(strategy?.input ?? null);

  // Connect status per source, from load(): a source with a connect step is
  // connected once its OAuth callback parked a token cookie.
  const connected = $derived((page.data.connected ?? {}) as Partial<Record<ImportSource, boolean>>);
  const sourceConnected = $derived(source ? connected[source] === true : false);
  let credential = $state('');
  let uploadFile = $state<File | null>(null);
  let dragKind = $state<'' | ImportSource>('');
  let submitting = $state(false);

  let previewResult = $state<PreviewResponse | null>(null);
  let commitResult = $state<CommitResponse | null>(null);
  let finishError = $state('');
  let finishing = $state(false);

  const ORDER: Step[] = ['pick', 'instructions', 'commands', 'extras', 'review', 'done'];

  // The settings importer's headings carry that page's own four-step count
  // ("3 · Review…"). This journey has six stages and numbers them on the
  // rail and the companion, so the borrowed prefix is dropped here rather
  // than contradicting both.
  const unnumbered = (s: string) => s.replace(/^\s*\d+\s*·\s*/, '');
  const stepIndex = $derived(ORDER.indexOf(step));
  const journeyStep = $derived(BEFORE_IMPORT.length + stepIndex);
  const journeyLabels = $derived([...BEFORE_IMPORT, ...STAGES]);
  // The rail only ever travels back: every stage ahead needs the one before
  // it to have produced something (a source, a preview, a commit).
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

  // ── Motion ───────────────────────────────────────────────────────────
  // Same curve and travel as /welcome, so the two routes move as one.
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

  // Bolota's face and one-shots. A timed reaction outranks a pointer hover,
  // which outranks the stage's resting face; same precedence as /welcome.
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
    // Swirl preserves the base face so Bolota can keep watching the pointer
    // while the stage itself slides in the travel direction.
    play('entrance');
  }

  onMount(() => {
    return () => {
      if (reactionTimer) clearTimeout(reactionTimer);
    };
  });

  // Pointer parallax for the sky, coalesced to one write per frame.
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
    source = s;
    uploadFile = null;
    credential = '';
    react('excited', 1400);
    // Picking a tile advances to that source's how-to-find-it instructions;
    // the credential/file input lives there now, not on the tile.
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

  // --- review selection ----------------------------------------------------
  // Checked items land in the committed manifest; anything carrying an error
  // diagnostic cannot land at all (commit drops those server-side too), so it
  // renders pre-unchecked with the reason on its row.
  type RowKind = 'commands' | 'timers' | 'triggers' | 'quotes';

  let selected = $state<Record<string, boolean>>({});
  let overwrite = $state(false);

  // Diagnostic codes are prefixed with the collection they address
  // (CONTRACT §5); matching on the prefix + item index is what puts each
  // warning badge on the right row.
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

  // Initialize once per preview. Changing stages must preserve the choices.
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

  // Collisions: normalized names of existing channel items the import would
  // land on top of. Matching rows highlight until the user opts into
  // overwriting; FindCollisions normalizes server-side exactly like this.
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

  // Non-zero stat chips for the strip atop the review step.
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

  // --- bulk selection, commit-bar counter, rail detail lines ----------------
  // "Select all" still refuses error-flagged rows: commit drops those
  // server-side, so checking them would promise a landing that never happens.
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

  // One line of detail per rail stage, so the rail reports the actual choices
  // (source, file/token, selection) instead of repeating the stage names.
  const railDetail = $derived.by(() => [
    strategy ? strategy.label : t('import.railPickPending'),
    inputDetail(),
    previewResult ? t('onboardingImport.commandsCount', { n: previewResult.manifest?.commands?.length ?? 0 }) : '',
    previewResult ? statsLine : '',
    previewResult ? selectionLine : '',
    commitResult ? t('import.railDone') : ''
  ]);

  // The second rail line reports what the picked source actually takes: the
  // typed value for a text source, the chosen file for everything else (a
  // connect-first source has neither, and reads as a pending file exactly as
  // it did before this line stopped naming StreamElements).
  function inputDetail(): string {
    if (inputSpec?.kind === 'text') return textInputDetail(inputSpec);
    return uploadFile ? uploadFile.name : t('import.railFilePending');
  }

  // A secret and a public channel name are not the same promise to the reader,
  // so the rail names what it is holding rather than calling a handle a token.
  function textInputDetail(spec: TextInputSpec): string {
    if (spec.secret) return credential ? t('import.railTokenSet') : t('import.railTokenPending');
    return credential ? t('import.railHandleSet') : t('import.railHandlePending');
  }

  // Count tiles on the done panel: only collections that actually landed.
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

  // Build the commit payload from what is still checked, dropping collections
  // that end up empty (mirrors ImportManifest's omitempty shape).
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
    // Automod terms have no row of their own, so they used to ride along even
    // when every row was unchecked: the commit bar could read "0 of N selected"
    // and the commit would still call commitAutomodTerms. Nothing selected now
    // means nothing imported.
    if (m.automod && rowPicked > 0) out.automod = m.automod;
    return JSON.stringify(out);
  }

  // --- form handlers -------------------------------------------------------
  // Instructions content per source: numbered steps are list leaves, named by
  // the source's own strategy.
  const instrSteps = $derived.by(() => {
    const key = strategy?.i18n.instr;
    return key ? tl(key) : [];
  });

  // Seeded from the ?e= a source's connect routes bounce back with, so the
  // failure reads inline on the instructions step the deep link reopens.
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
      // Caps fire client-side only (the overflow never reached the server), so
      // those warnings are stapled in front of the server's own.
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

  // PreparedInput is what one input kind contributed to the post: either a
  // refusal to show inline, or the diagnostics a browser-side parse produced
  // (empty for every source the server parses).
  type PreparedInput = { error: string } | { diags: ImportDiagnostic[] };

  function prepareInput(spec: InputSpec, body: FormData): Promise<PreparedInput> | PreparedInput {
    if (spec.kind === 'text') return prepareText(spec, body);
    if (spec.kind === 'oauth') return prepareOauth(spec);
    return prepareFile(spec, body);
  }

  // prepareText posts the pasted value as `credential`. The shape gate is the
  // source's own (the StreamElements JWT's three base64url segments today), so
  // an obvious typo is answered without a round trip; the action re-checks it.
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

  // prepareOauth posts no inputs at all: the server reads the access token off
  // the HttpOnly cookie the connect flow parked, then fetches the account's
  // config itself.
  function prepareOauth(spec: OAuthInputSpec): PreparedInput {
    if (!sourceConnected) return { error: t(spec.i18n.errNotConnected) };
    return { diags: [] };
  }

  // prepareFile refuses an oversized file before reading a byte of it. The
  // ceiling is the source's own and mirrors/precedes the server's: 10MB for
  // the Moobot JSON this page parses itself, 20MB for the StreamLabs .db that
  // still uploads whole because console CSP forbids WASM (no
  // 'wasm-unsafe-eval' in script-src, see shared/svelte-config.js), which
  // rules out an in-browser SQLite reader. Decision record: loosening the CSP
  // was weighed and rejected, one source keeping its server path costs less
  // than widening script-src for every dashboard visitor.
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

  // parseInPage decodes an export in the browser: JSON.parse inside the parser
  // (never eval), with per-item degradation. Only the resulting manifest rides
  // to the server, which re-validates it through validateManifest for
  // authoritative diagnostics, collisions and stats, so the raw file never
  // leaves the machine it was exported on.
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

  // parseErrorMessage surfaces a parser's own refusal. Every parser prefixes
  // its messages with `importer/<source>: `, which is both how one is
  // recognized here and what gets stripped before the prose is shown; anything
  // else that went wrong reading the file reads as the generic failure.
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

  // Finishing is the save and the completion beat running side by side: the
  // circle starts drawing the moment the button is pressed, and the dashboard
  // opens once both the beat has played and the save has landed. A failed
  // save pulls the overlay straight back so the error reads on the done
  // scene instead of behind a closing animation.
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

  // Courtesy gate at drop time: fail fast on an obviously wrong file type so
  // the user gets a clear message instead of a parse failure after submit.
  // NOT a security control: extensions are trivially spoofed either way; the
  // authoritative checks stay content-based (JSON envelope shape for Moobot,
  // SQLite magic bytes + feature-table probe for StreamLabs).
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

  // A file spec's accept list leads with the extension and follows with the
  // MIME type ('.json,application/json'), so the drop-time check reads the
  // first entry rather than carrying a second table of its own.
  function wantedExtension(): string {
    if (inputSpec?.kind !== 'file') return '';
    const first = inputSpec.accept.split(',')[0];
    return first.startsWith('.') ? first : '';
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
<div class="welcome-import" class:leaving={showCompletion} data-welcome>
  <header class="top">
    <Brand title="ItsBagelBot" sub={t('common.console')} logoSrc="/logo.png" logoAlt="" size="md" />
    <StepRail labels={journeyLabels} current={journeyStep} maxStep={journeyStep} label={t('onboarding.stepOf', { n: journeyStep + 1, total: journeyLabels.length })} onselect={selectRail} />
  </header>
<section class="screen active">
  <div class="intro">
    <!-- The companion is the thread through the wizard: the same seeded face
         as /welcome, swinging to the side of the current stage, reacting to
         what just happened, and holding the stage name and what has been
         chosen for it. Decorative: the rail below says the same in text. -->
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
  <!-- One scene per stage, stacked in a single grid cell so the leaving
       stage and the arriving one cross instead of stacking for a frame. -->
  {#key step}
  <div class="scene" in:sceneIn out:sceneOut>

  {#if step === 'pick'}
    <Card>
      <Heading level={2} class="step-title">{unnumbered(t('import.stepPick'))}</Heading>
      <p class="hint">{t('import.pickHint')}</p>

      <div class="tiles">
        <!-- One tile per registered source, in IMPORT_SOURCES order. Tiles are
             pure selectors: picking one advances to that source's how-to-find-it
             instructions, where its credential/file input lives. A source whose
             input is not built yet ships visibly disabled rather than
             half-working, and cannot be picked or deep-linked into. -->
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
    <Card>
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
          <!-- A secret is pasted (a JWT wraps over several lines and wants the
               room), a public handle is typed: one short word, where a
               textarea reads as "paste something big here" and accepts a
               newline the gate then refuses. Same binding either way. -->
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
            <input
              class="bb-input bb-input--fill cred-mono"
              type="text"
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
        <!-- Drag handlers sit on the input, not the wrapper: the input covers
             the whole zone invisibly, so behaviour is identical while the
             wrapper needs no interactive ARIA role. -->
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
    <Card class="review-head">
      <Heading level={2} class="step-title">{unnumbered(t('import.reviewTitle'))}</Heading>
      <p class="hint">{reviewHint}</p>

      <div class="review-bar">
        {#each statChips as c (c)}<Tag tone="quiet">{c}</Tag>{/each}
        <span class="review-spacer"></span>
        <Button type="button" variant="ghost" size="sm" onclick={() => setAll(true)}>{t('import.selectAll')}</Button>
        <Button type="button" variant="ghost" size="sm" onclick={() => setAll(false)}>{t('import.selectNone')}</Button>
      </div>
    </Card>
    {:else}
      <div class="category-head">
        <span class="eyebrow">{step === 'commands' ? t('onboardingImport.commandsEyebrow') : t('onboardingImport.extrasEyebrow')}</span>
        <h2>{step === 'commands' ? t('onboardingImport.commandsTitle') : t('onboardingImport.extrasTitle')}</h2>
        <p>{step === 'commands' ? t('onboardingImport.commandsBody') : t('onboardingImport.extrasBody')}</p>
      </div>
      {#if step === 'extras'}
        <div class="module-note" role="note">
          <strong>{t('onboardingImport.modulesTitle')}</strong>
          <p>{t('onboardingImport.modulesBody')}</p>
        </div>
      {/if}
    {/if}

      {#each manifestLevelDiags as d (d.code + d.message)}
        <p class="manifest-warn" role="status">{d.message}</p>
      {/each}

      {#if anyCollisions}
        <div class="collision-note">
          {@html t('import.conflictsNote', { n: previewResult.collisions?.length ?? 0 })}
          <span class="overwrite-toggle">
            <Checkbox bind:checked={overwrite} name="overwrite" value="on">{t('import.overwriteToggle')}</Checkbox>
          </span>
        </div>
      {/if}

      {#if step === 'commands' && previewResult.manifest.commands?.length}
        <Card class="group">
          <div class="group-head">
            <span class="group-title">{t('import.hCommands')}</span>
            <span class="group-count">{previewResult.manifest.commands.length}</span>
          </div>
          <ul class="rows">
            {#each previewResult.manifest.commands as c, i (c.name)}
              {@const diags = itemDiags('commands', i)}
              <li class="row-item" class:collision={collidedCommands.has(normalizeName(c.name))}>
                <span class="pick">
                  <Checkbox bind:checked={() => isChecked('commands', i), (on) => toggle('commands', i, on)}><span class="row-name">!{c.name}</span></Checkbox>
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
        <Card class="group">
          <div class="group-head">
            <span class="group-title">{t('import.hTimers')}</span>
            <span class="group-count">{previewResult.manifest.timers.length}</span>
          </div>
          <ul class="rows">
            {#each previewResult.manifest.timers as tm, i (tm.message)}
              {@const diags = itemDiags('timers', i)}
              <li class="row-item">
                <span class="pick">
                  <Checkbox bind:checked={() => isChecked('timers', i), (on) => toggle('timers', i, on)} aria-label={tm.message} />
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
        <Card class="group">
          <div class="group-head">
            <span class="group-title">{t('import.hTriggers')}</span>
            <span class="group-count">{previewResult.manifest.triggers.length}</span>
          </div>
          <ul class="rows">
            {#each previewResult.manifest.triggers as tg, i (tg.phrase)}
              {@const diags = itemDiags('triggers', i)}
              <li class="row-item">
                <span class="pick">
                  <Checkbox bind:checked={() => isChecked('triggers', i), (on) => toggle('triggers', i, on)}><span class="row-name">{tg.phrase}</span></Checkbox>
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
        <Card class="group">
          <div class="group-head">
            <span class="group-title">{t('import.hQuotes')}</span>
            <span class="group-count">{previewResult.manifest.quotes.length}</span>
          </div>
          <p class="hint">{t('import.quotesAll', { n: previewResult.manifest.quotes.length })}</p>
        </Card>
      {/if}

      {#if commitError}<AlertBanner>{commitError}</AlertBanner>{/if}

      <!-- The last stage commits only after every category has been reviewed. -->
      {#if step === 'review'}
      <form
        class="commit-bar"
        onsubmit={(e) => {
          e.preventDefault();
          runCommit();
        }}
      >
        <span class="commit-line">{selectionLine}</span>
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
    <Card class="done-panel">
      <!-- The companion above is doing the celebrating (same seeded face as
           /welcome); this seal is the scene's own quiet confirmation. -->
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
          {#if commitResult.skipped?.length}
            {t('import.skippedLine', {
              n: commitResult.skipped.length,
              names: commitResult.skipped.map((c) => c.name).join(', ')
            })}
          {/if}
        </p>
        {#if appliedTiles.length}
          <div class="applied">
            {#each appliedTiles as a (a.label)}
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
      <div class="actions">
        <Button variant="primary" onclick={finishOnboarding} loading={finishing}>{t('onboardingImport.dashboard')}</Button>
      </div>
      {#if finishError}<AlertBanner>{finishError}</AlertBanner>{/if}
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
{#if showCompletion}<Completion label={t('onboardingImport.stageDone')} oncomplete={afterCompletion} />{/if}

<style>
  /* Same rule as /welcome: this page paints its own sky, so the shell's
     ambient orb pair would muddy it. Scoped to this page's presence. */
  :global(body:has([data-welcome]) .bb-bg-orb) { display: none; }

  .welcome-import {
    position: relative;
    z-index: 1;
    min-height: 100vh;
    padding: 0 clamp(18px, 5vw, 72px) 72px;
    color: var(--bb-white);
    overflow-x: clip;
  }
  /* `backwards`, not `both`: a held final keyframe would pin opacity at 1
     and the leaving fade below could never apply. */
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

  /* Companion: blob plus a speech bubble naming the stage. The pair swaps
     order on alternate stages (the /welcome side swap, in miniature), as a
     transform so the row never reflows. */
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
  /* The float loop lives one level in from the side swap: both are
     transforms and one element cannot run a transition and a keyframe loop
     on the same property. The phone size uses `scale`, a separate property,
     so it composes with the loop. */
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
  .category-head h2 {
    margin: 0 0 12px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    letter-spacing: -.04em;
    line-height: 1.08;
  }
  .category-head p { margin: 0; color: var(--bb-muted); line-height: 1.6; }
  .category-head {
    padding: 26px 30px;
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-lg);
    background: rgba(17, 17, 16, .76);
    backdrop-filter: blur(18px);
  }
  .category-head h2 { font-size: clamp(25px, 3vw, 36px); }
  .module-note {
    padding: 18px 22px;
    border: 1px solid rgba(201, 168, 124, .38);
    border-radius: var(--bb-radius-md);
    background: rgba(201, 168, 124, .07);
  }
  .module-note strong { color: var(--bb-tan-pale); }
  .module-note p { margin: 6px 0 0; color: var(--bb-muted); line-height: 1.55; }
  .category-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    padding-top: 8px;
  }
  :global(.welcome-import .scene > .bb-card) {
    background: rgba(17, 17, 16, .76);
    backdrop-filter: blur(18px);
  }
  :global(:root[data-theme="light"]) .bubble,
  :global(:root[data-theme="light"]) .category-head,
  :global(:root[data-theme="light"] .welcome-import .scene > .bb-card) {
    background: rgba(255, 255, 255, .72);
  }

  @media (max-width: 760px) {
    .top { flex-wrap: wrap; padding: 18px 0; }
    :global(.welcome-import .screen) { margin-top: 24px; }
  }
  /* Tablets and phones: the companion stays (it is the thread through the
     journey) but shrinks into a compact row above the heading, blob beside
     its bubble, once the side-by-side row would squeeze the heading. */
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

  /* The step titles are `Heading` blocks. 16px is between the l4 (20px) and
     l5 (17px) steps, and matches the settings page's own section heads: this
     page is one of its sections. */
  :global(.step-title) { margin-bottom: 6px; font-size: 16px; }
  .hint {
    color: var(--bb-muted, #888077);
    font-size: 13px;
    margin: 0 0 12px;
  }

  /* The imported stages use the same single stage width as onboarding. */
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
  /* --- step 1: source tiles --- */  .tiles {
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
  /* Keyboard access: the real radio stays in the tab order, and :has() lifts
     the ring onto the tile when it receives focus-visible. */
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

  /* --- step 2: per-source instructions --- */
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
  /* The link look is `.bb-prose` on the paragraph
     (@bagel/ui/styles/elements/typography.css). Local: this one link is the
     step's whole instruction, so it is underlined at rest rather than on
     hover -- a reader following a numbered step should not have to find it. */
  .instr-link a { text-decoration: underline; text-underline-offset: 2px; }
  .cred {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-bottom: 6px;
  }
  /* Keyed on the CTA's own class: where THIS button sits in the column. */
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
  /* The controls are `Textarea` and `.bb-input` now. This file used to draw
     the frame itself, at its own padding and its own background: the fifth
     redrawing of a control the library ships. What is genuinely local is the
     FACE: a credential is a token, so its characters have to be
     distinguishable (l vs 1, O vs 0) in a way prose does not need. */
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
  /* The native control covers the zone invisibly so click, keyboard focus and
     the OS picker all stay native; focus draws the ring on the zone. */
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

  /* --- step 2: stats strip + review rows --- */
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
  /* --- design import: tile CTA, instruction head, review + done panels --- */
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

  /* Each collection is its own panel, so a long commands list cannot push a
     later heading out of sight of its own rows. */
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
  /* Lateral, like /welcome's: the resting blob hovers rather than bobs. */
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
