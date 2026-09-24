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
    Heading,
    Icon,
    PageHead,
    Tag,
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
  let dragKind = $state<'' | ImportSource>('');
  let submitting = $state(false);

  let previewResult = $state<PreviewResponse | null>(null);
  let commitResult = $state<CommitResponse | null>(null);

  const stepIndex = $derived(
    step === 'pick' ? 0 : step === 'instructions' ? 1 : step === 'review' ? 2 : 3
  );

  function choose(s: ImportSource) {
    source = s;
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
    <Card as="aside" class="rail" aria-label={t('import.stagesLabel')}>
      <p class="rail-head">{t('import.railProgress')}</p>
      <ol class="rail-list">
        {#each STAGES as key, i (key)}
          <li
            class="rail-item"
            class:done={i < stepIndex}
            class:current={i === stepIndex}
            aria-current={i === stepIndex ? 'step' : undefined}
          >
            <span class="rail-gutter" aria-hidden="true">
              <span class="rail-dot">
                {#if i < stepIndex}<Icon name="check" size={11} />{:else}{i + 1}{/if}
              </span>
              {#if i < STAGES.length - 1}<span class="rail-bar"></span>{/if}
            </span>
            <span class="rail-text">
              <span class="rail-title">{t(key)}</span>
              {#if railDetail[i]}<span class="rail-detail">{railDetail[i]}</span>{/if}
            </span>
          </li>
        {/each}
      </ol>
      <p class="rail-foot">{t('import.railAudit')}</p>
    </Card>

    <div class="flow">

  {#if step === 'pick'}
    <Card>
      <Heading level={2} class="step-title">{t('import.stepPick')}</Heading>
      <p class="hint">{t('import.pickHint')}</p>

      <div class="tiles">
        {#each IMPORT_SOURCES as id (id)}
          {@const s = IMPORT_STRATEGIES[id]}
          {#if s.available}
            <label class="tile" class:picked={source === id} data-cursor>
              <input
                type="radio"
                name="source-pick"
                value={id}
                checked={source === id}
                onchange={() => choose(id)}
              />
              <span class="tile-top">
                <span class="glyph" aria-hidden="true">{s.initials}</span>
                <span class="tile-name">{s.label}</span>
                <Tag tone="pre">{t(CHIP_LABEL_KEYS[s.chip])}</Tag>
              </span>
              <span class="tile-desc">{t(s.i18n.desc)}</span>
              <span class="tile-cta">{t('import.tileCta')}</span>
            </label>
          {:else}
            <div class="tile disabled" aria-disabled="true">
              <span class="tile-top">
                <span class="glyph" aria-hidden="true">{s.initials}</span>
                <span class="tile-name">{s.label}</span>
                <Tag tone="quiet">{t('import.chipSoon')}</Tag>
              </span>
              <span class="tile-desc">{t(s.i18n.desc)}</span>
            </div>
          {/if}
        {/each}
      </div>
    </Card>
  {:else if step === 'instructions' && source}
    {@const st = IMPORT_STRATEGIES[source]}
    {@const spec = st.input}
    <Card>
      <div class="instr-head">
        <span class="glyph" aria-hidden="true">{st.initials}</span>
        <Heading level={2} class="step-title">{t('import.stepInstructions', { source: st.label })}</Heading>
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
            <ButtonLink href={spec.connectPath} variant="primary" class="cred-cta">
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
            }}
            ondragleave={() => (dragKind = '')}
            ondrop={(e) => {
              e.preventDefault();
              dragKind = '';
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
    <Card class="review-head">
      <Heading level={2} class="step-title">{t('import.reviewTitle')}</Heading>
      <p class="hint">{reviewHint}</p>

      <div class="review-bar">
        {#each statChips as c (c)}<Tag tone="quiet">{c}</Tag>{/each}
        <span class="review-spacer"></span>
        <Button type="button" variant="ghost" size="sm" onclick={() => setAll(true)}>{t('import.selectAll')}</Button>
        <Button type="button" variant="ghost" size="sm" onclick={() => setAll(false)}>{t('import.selectNone')}</Button>
      </div>
    </Card>

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

      {#if previewResult.manifest.commands?.length}
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
                  <span class="row-response row-response--source">{sourceLine(c)}</span>
                  <span class="row-response">
                    {#each segmentResponse(c.responses?.join(' / ') ?? '') as seg, si (si)}
                      {#if seg.flagged}<Tag tone="alpha" class="bb-tag--literal">{seg.text}</Tag>{:else}{seg.text}{/if}
                    {/each}
                  </span>
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

      {#if previewResult.manifest.timers?.length}
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

      {#if previewResult.manifest.triggers?.length}
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

      {#if previewResult.manifest.quotes?.length}
        <Card class="group">
          <div class="group-head">
            <span class="group-title">{t('import.hQuotes')}</span>
            <span class="group-count">{previewResult.manifest.quotes.length}</span>
          </div>
          <p class="hint">{t('import.quotesAll', { n: previewResult.manifest.quotes.length })}</p>
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
  {:else if step === 'done'}
    <Card class="done-panel">
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
      <Heading level={2} class="step-title">{t('import.doneTitle')}</Heading>
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
        <Button variant="primary" onclick={reset}>{t('import.backToSources')}</Button>
      </div>
      {#if commitResult?.audit_id}
        <p class="audit">{t('import.auditFoot', { n: commitResult.audit_id })}</p>
      {/if}
    </Card>
  {/if}
    </div>
  </div>
</section>

<style>
  :global(.step-title) { margin-bottom: 6px; font-size: 16px; }
  .hint {
    color: var(--bb-muted, #888077);
    font-size: 13px;
    margin: 0 0 12px;
  }

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
  :global(.rail) {
    position: sticky;
    top: 32px;
    padding: 22px 20px;
  }
  .rail-head {
    margin: 0 0 18px;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .rail-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .rail-item {
    display: flex;
    gap: 12px;
    align-items: flex-start;
  }
  .rail-gutter {
    display: flex;
    flex-direction: column;
    align-items: center;
    flex: none;
    width: 26px;
  }
  .rail-dot {
    width: 26px;
    height: 26px;
    border-radius: 50%;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    border: 1px solid var(--glass-border);
    background: var(--glass-fill);
    color: var(--bb-muted);
    transition:
      border-color var(--bb-dur-fast, 140ms) ease,
      color var(--bb-dur-fast, 140ms) ease;
  }
  .rail-bar {
    width: 1px;
    flex: 1;
    min-height: 26px;
    background: var(--bb-border);
  }
  .rail-text {
    padding-bottom: 18px;
    min-width: 0;
  }
  .rail-title {
    display: block;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .rail-detail {
    display: block;
    margin-top: 5px;
    font-size: 12.5px;
    line-height: 1.45;
    color: #5f5a53;
    overflow-wrap: anywhere;
  }
  .rail-item.current .rail-title {
    color: var(--bb-white);
  }
  .rail-item.current .rail-dot {
    border-color: rgba(201, 168, 124, 0.6);
    color: var(--bb-tan-light);
    box-shadow: 0 0 0 3px rgba(201, 168, 124, 0.12);
  }
  .rail-item.done .rail-title {
    color: var(--bb-green-glow, #52b788);
  }
  .rail-item.done .rail-dot {
    border-color: rgba(82, 183, 136, 0.5);
    color: var(--bb-green-glow, #52b788);
  }
  .rail-foot {
    margin: 6px 0 0;
    padding-top: 18px;
    border-top: 1px solid var(--bb-border);
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--bb-muted);
  }

  @media (max-width: 900px) {
    .wizard {
      grid-template-columns: minmax(0, 1fr);
    }
    :global(.rail) {
      position: static;
    }
  }

  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
    gap: 12px;
    margin: 16px 0 4px;
  }
  .tile {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 8px;
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-md);
    padding: 16px 18px;
    background: var(--glass-fill);
    cursor: pointer;
    transition:
      border-color 200ms ease,
      background 200ms ease,
      box-shadow 200ms ease;
  }
  .tile.picked {
    border-color: rgba(201, 168, 124, 0.65);
    background: rgba(201, 168, 124, 0.05);
    box-shadow:
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
    gap: 10px;
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
      color 200ms ease;
  }
  .tile.picked .glyph {
    border-color: rgba(201, 168, 124, 0.55);
    color: var(--bb-tan);
  }
  .tile-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 14.5px;
    color: var(--bb-white);
    flex: 1;
    min-width: 0;
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
    height: calc(13px * 1.5);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .row-response--source {
    opacity: 0.6;
    font-style: italic;
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

  .done-blob {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 76px;
    height: 76px;
    border-radius: var(--bb-radius-lg);
    background: rgba(82, 183, 136, 0.12);
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
</style>
