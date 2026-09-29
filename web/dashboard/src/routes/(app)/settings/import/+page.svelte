<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { page } from '$app/state';
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
    Input,
    Label,
    PageHead,
    RadioGroup,
    StatTile,
    Stepper,
    Tag,
    Text,
    TextLink,
    Textarea,
    toast,
    getI18n
  } from '@bagel/kit';
  import { applyImportCaps } from '@bagel/kit/importer/caps';
  import { localizeImporterError } from '$lib/importer-errors';
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

  const { t, tl } = getI18n();

  const STAGES = [
    'import.stagePick',
    'import.stageInstructions',
    'import.stageReview',
    'import.stageDone'
  ] as const;

  type Step = 'pick' | 'instructions' | 'review' | 'done';
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

  const stepIndex = $derived(
    step === 'pick' ? 0 : step === 'instructions' ? 1 : step === 'review' ? 2 : 3
  );

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

  function choose(value: string) {
    if (!isImportSource(value)) return;
    source = value;
    uploadFile = null;
    credential = '';
    step = 'instructions';
  }

  function reset() {
    step = 'pick';
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

  const SOURCE_UNTRANSLATED_PATTERN: Record<ImportSource, RegExp> = {
    nightbot: /\$\([^)]*\)/g,
    fossabot: /\$\([^)]*\)/g,
    streamelements: /\$\([^)]*\)|\$\{[^}]*\}/g,
    moobot: /<[a-zA-Z0-9_.-]+>/g,
    wizebot: /\$\([^)]*\)|\$[a-zA-Z_]+\([^)]*\)/g,
    streamlabs_desktop: /\$[a-zA-Z_][a-zA-Z0-9_]*(\([^)]*\))?/g
  };

  interface ResponseSegment {
    text: string;
    flagged: boolean;
  }

  function segmentResponse(text: string): ResponseSegment[] {
    const pattern = source ? SOURCE_UNTRANSLATED_PATTERN[source] : null;
    if (!pattern) return [{ text, flagged: false }];
    const out: ResponseSegment[] = [];
    let last = 0;
    for (const m of text.matchAll(pattern)) {
      const start = m.index ?? 0;
      if (start > last) out.push({ text: text.slice(last, start), flagged: false });
      out.push({ text: m[0], flagged: true });
      last = start + m[0].length;
    }
    if (last < text.length) out.push({ text: text.slice(last), flagged: false });
    return out;
  }

  function sourceLine(c: ManifestCommand): string {
    return c.source_responses?.join(' / ') || ' ';
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

  $effect(() => {
    const m = previewResult?.manifest;
    if (!m || step !== 'review') return;
    const next: Record<string, boolean> = {};
    for (const kind of Object.keys(CODE_PREFIX) as RowKind[]) {
      const rows = (m[kind] as unknown[] | undefined) ?? [];
      for (let i = 0; i < rows.length; i++) {
        next[`${kind}:${i}`] = !itemDiags(kind, i).some((d) => d.severity === 'error');
      }
    }
    selected = next;
  });

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
    previewResult ? selectionLine : t('import.railReviewPending'),
    commitResult ? t('import.railDone') : ''
  ]);
  const railSteps = $derived(STAGES.map((key, i) => ({ label: t(key), detail: railDetail[i] })));

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
      submitting = false;
      return;
    }

    const r = await postPreview(body);
    if (r.ok && r.preview) {
      r.preview.diagnostics = [...prepared.diags, ...(r.preview.diagnostics ?? [])];
      previewResult = r.preview;
      step = 'review';
    } else {
      previewError = localizeImporterError(r.error, t) || t('import.errGeneric');
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
        commitError = localizeImporterError(d?.error, t) || t('import.errGeneric');
      } else if (r.type === 'success') {
        const d = r.data as { ok?: boolean; commit?: CommitResponse } | undefined;
        if (d?.ok && d.commit) {
          commitResult = d.commit;
          step = 'done';
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
      return;
    }
    uploadFile = f;
  }

  function wantedExtension(): string {
    if (inputSpec?.kind !== 'file') return '';
    const first = inputSpec.accept.split(',')[0];
    return first.startsWith('.') ? first : '';
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('settings.eyebrow')} description={t('import.pageDesc')}>
    {t('import.pageTitlePre')}{' '}<em>{t('import.pageTitleEm')}</em>
  </PageHead>

  <div class="wizard">
    <aside class="rail">
      <Card>
        <div class="rail-head"><Label mono as="span">{t('import.railProgress')}</Label></div>
        <Stepper orientation="vertical" steps={railSteps} current={stepIndex} label={t('import.stagesLabel')} />
        <div class="rail-foot"><Text size="xs" tone="muted">{t('import.railAudit')}</Text></div>
      </Card>
    </aside>

    <div class="flow">

  {#if step === 'pick'}
    <Card>
      <div class="step-title"><Heading level={6} as="h2">{t('import.stepPick')}</Heading></div>
      <div class="hint"><Text size="sm" tone="muted">{t('import.pickHint')}</Text></div>

      <div class="tiles">
        <RadioGroup
          variant="cards"
          min="250px"
          name="source-pick"
          label={t('import.stepPick')}
          value={source}
          options={sourceOptions}
          onchange={choose}
        >
          {#snippet lead(option, on)}
            {@const s = IMPORT_STRATEGIES[option.value as ImportSource]}
            <span class="glyph" class:picked={on} aria-hidden="true">{s.initials}</span>
            {#if s.available}<Tag tone="pre">{t(CHIP_LABEL_KEYS[s.chip])}</Tag>{:else}<Tag tone="quiet">{t('import.chipSoon')}</Tag>{/if}
          {/snippet}
        </RadioGroup>
      </div>
    </Card>
  {:else if step === 'instructions' && source}
    {@const st = IMPORT_STRATEGIES[source]}
    {@const spec = st.input}
    <Card>
      <div class="instr-head">
        <span class="glyph" aria-hidden="true">{st.initials}</span>
        <Heading level={6} as="h2">{t('import.stepInstructions', { source: st.label })}</Heading>
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
            <ButtonLink href={spec.connectPath} variant="primary">
              {t(spec.i18n.cta)}
            </ButtonLink>
          {/if}
          <div class="hint"><Text size="sm" tone="muted">{t(spec.i18n.scopeHint)}</Text></div>
        </div>
      {:else}
        <FileDrop label={t('import.dropHint')} accept={spec.accept} file={uploadFile} onfile={pickFile} />
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
          <Button variant="ghost" type="button" onclick={() => (step = 'pick')} disabled={submitting}
            >{t('import.back')}</Button
          >
          <Button type="submit" variant="primary" loading={submitting}>
            {t('import.continueCta')}
          </Button>
        </div>
      </form>
    </Card>
  {:else if step === 'review' && previewResult?.manifest}
    <Card>
      <div class="step-title"><Heading level={6} as="h2">{t('import.reviewTitle')}</Heading></div>
      <div class="hint"><Text size="sm" tone="muted">{reviewHint}</Text></div>

      <div class="review-bar">
        {#each statChips as c (c)}<Tag tone="quiet">{c}</Tag>{/each}
        <span class="review-spacer"></span>
        <Button type="button" variant="ghost" size="sm" onclick={() => setAll(true)}>{t('import.selectAll')}</Button>
        <Button type="button" variant="ghost" size="sm" onclick={() => setAll(false)}>{t('import.selectNone')}</Button>
      </div>
    </Card>

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

      {#if previewResult.manifest.commands?.length}
        <Card flush>
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
                  <span class="row-source"><Text as="span" size="sm" tone="muted" truncate>{sourceLine(c)}</Text></span>
                  <Text as="span" size="sm" tone="muted" truncate>
                    {#each segmentResponse(c.responses?.join(' / ') ?? '') as seg, si (si)}
                      {#if seg.flagged}<Tag tone="alpha" literal>{seg.text}</Tag>{:else}{seg.text}{/if}
                    {/each}
                  </Text>
                  <span class="chips">
                    {#if c.permission && c.permission !== 'everyone'}<PermBadge perm={c.permission} />{/if}
                    {#if c.cooldown_seconds}<Tag tone="bare">{t('import.cooldownChip', { n: c.cooldown_seconds })}</Tag>{/if}
                    {#each c.aliases ?? [] as a (a)}<Tag tone="bare" literal>!{a}</Tag>{/each}
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

      {#if previewResult.manifest.timers?.length}
        <Card flush>
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
                  <Text as="span" size="sm" tone="muted" truncate>{tm.message}</Text>
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

      {#if previewResult.manifest.triggers?.length}
        <Card flush>
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
                  <Text as="span" size="sm" tone="muted" truncate>{tg.response}</Text>
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

      {#if previewResult.manifest.quotes?.length}
        <Card flush>
          <div class="group-head">
            <Eyebrow>{t('import.hQuotes')}</Eyebrow>
            <Text as="span" size="xs" mono tone="muted">{previewResult.manifest.quotes.length}</Text>
          </div>
          <div class="group-note"><Text size="sm" tone="muted">{t('import.quotesAll', { n: previewResult.manifest.quotes.length })}</Text></div>
        </Card>
      {/if}

      {#if commitError}<AlertBanner>{commitError}</AlertBanner>{/if}

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
  {:else if step === 'done'}
    <Card>
      <span class="done-blob">
        <Bolota
          name={page.data.displayName ?? page.data.login ?? 'ItsBagelBot'}
          size={58}
          active={true}
          cycle={false}
          sequence="entrance"
          sequenceKey={commitResult?.audit_id ?? 'done'}
        />
      </span>
      <div class="step-title"><Heading level={6} as="h2">{t('import.doneTitle')}</Heading></div>
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
          <div class="applied">
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
        <Button variant="primary" onclick={reset}>{t('import.backToSources')}</Button>
      </div>
      {#if commitResult?.audit_id}
        <div class="audit"><Text size="xs" mono tone="muted">{t('import.auditFoot', { n: commitResult.audit_id })}</Text></div>
      {/if}
    </Card>
  {/if}
    </div>
  </div>
</section>

<style>
  .step-title { margin-bottom: 6px; }
  .hint { margin: 0 0 12px; }

  .wizard {
    display: grid;
    grid-template-columns: 264px minmax(0, 1fr);
    gap: 28px;
    align-items: start;
  }
  .flow {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
  .rail {
    position: sticky;
    top: 32px;
  }
  .rail-head { margin-bottom: 18px; }
  .rail-foot {
    margin-top: 6px;
    padding-top: 18px;
    border-top: 1px solid var(--bb-border);
  }

  @media (max-width: 900px) {
    .wizard {
      grid-template-columns: minmax(0, 1fr);
    }
    .rail {
      position: static;
    }
  }

  .tiles { margin: 16px 0 4px; }
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
  }
  .row-source {
    min-width: 0;
    opacity: 0.6;
    font-style: italic;
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

  .done-blob {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 76px;
    height: 76px;
    border-radius: var(--bb-radius-lg);
    background: rgba(var(--bb-green-glow-rgb), 0.12);
    border: 1px solid var(--bb-border-strong);
    margin-bottom: 14px;
  }
  .applied {
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
</style>
