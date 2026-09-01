<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // Config-import page: a first-class four-step flow (choose source,
  // per-source instructions, review, done) with the whole wizard internalized
  // in this route so nothing outside imports it. Deep-linkable via ?source=
  // (preselects that bot's card).
  //
  // Actions are hit with fetch + devalue (`/settings/import?/…`) instead of
  // `use:enhance`: enhance funnels results into whatever `form` prop the
  // CURRENT page load has, and keeping the posts manual means the step state
  // machine below fully owns when review/done render, with no reload wiping it.
  // Wire shapes come from @bagel/shared — single source:
  // console/shared/lib/importer/types.ts since the importer service folded
  // into the dashboard.
  //
  // Client-side parsing: the Moobot export is decoded and translated HERE
  // (lib/importer/moobot.ts, pinned against the Go parser it was ported from)
  // and only the resulting manifest is POSTed — raw files no longer cross the
  // wire for that source. StreamLabs .db stays a server-side upload because
  // console CSP forbids WASM (no wasm-unsafe-eval in script-src), which rules
  // out an in-browser SQLite reader; see the decision record at runPreview.

  import { page } from '$app/state';
  import { deserialize } from '$app/forms';
  import {
    AlertBanner,
    Badge,
    Bolota,
    Button,
    Card,
    Icon,
    PageHead,
    toast,
    getI18n
  } from '@bagel/shared';
  import { parseMoobot, MoobotExportError } from '@bagel/shared/importer/moobot';
  import { parseNightbot, NightbotExportError } from '@bagel/shared/importer/nightbot';
  import { applyImportCaps } from '@bagel/shared/importer/caps';
  import type {
    CommitResponse,
    ImportDiagnostic,
    ImportSource,
    ManifestCommand,
    ManifestCounter,
    ManifestQuote,
    ManifestTimer,
    ManifestTrigger,
    PreviewResponse
  } from '@bagel/shared';

  const { t, tl } = getI18n();

  // Sources that can actually be picked. Fossabot is excluded: its parser
  // exists backend-side but is unregistered and its OAuth connect flow is
  // unbuilt, so a deep link asking for it falls back to the plain picker.
  const PICKABLE: readonly ImportSource[] = [
    'streamelements',
    'moobot',
    'nightbot',
    'streamlabs_desktop'
  ];

  // Sources whose export is a JSON file this page parses itself, so the raw
  // file never leaves the browser. Everything else either fetches server-side
  // (StreamElements) or must upload (StreamLabs' SQLite .db, CSP no-go for a
  // browser-side reader).
  const CLIENT_PARSED = {
    moobot: parseMoobot,
    nightbot: parseNightbot
  } as const;

  function isClientParsed(s: ImportSource | ''): s is keyof typeof CLIENT_PARSED {
    return s === 'moobot' || s === 'nightbot';
  }

  const SOURCE_LABEL: Record<ImportSource, string> = {
    streamelements: 'StreamElements',
    fossabot: 'Fossabot',
    moobot: 'Moobot',
    nightbot: 'Nightbot',
    streamlabs_desktop: 'StreamLabs Chatbot'
  };
  const SOURCE_INITIALS: Record<ImportSource, string> = {
    streamelements: 'SE',
    fossabot: 'F',
    moobot: 'M',
    nightbot: 'NB',
    streamlabs_desktop: 'SL'
  };

  const STAGES = [
    'import.stagePick',
    'import.stageInstructions',
    'import.stageReview',
    'import.stageDone'
  ] as const;

  // --- step state ----------------------------------------------------------
  type Step = 'pick' | 'instructions' | 'review' | 'done';
  let step = $state<Step>('pick');
  // svelte-ignore state_referenced_locally
  let source = $state<ImportSource | ''>(
    (() => {
      const q = page.url.searchParams.get('source');
      return q && (PICKABLE as readonly string[]).includes(q) ? (q as ImportSource) : '';
    })()
  );
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
    // Picking a tile advances to that source's how-to-find-it instructions;
    // the credential/file input lives there now, not on the tile.
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

  // --- review selection ----------------------------------------------------
  // Checked items land in the committed manifest; anything carrying an error
  // diagnostic cannot land at all (commit drops those server-side too), so it
  // renders pre-unchecked with the reason on its row.
  type RowKind = 'commands' | 'timers' | 'triggers' | 'quotes' | 'counters';

  let selected = $state<Record<string, boolean>>({});
  let overwrite = $state(false);

  // Diagnostic codes are prefixed with the collection they address
  // (CONTRACT §5); matching on the prefix + item index is what puts each
  // warning badge on the right row.
  const CODE_PREFIX: Record<RowKind, string> = {
    commands: 'command',
    timers: 'timer',
    triggers: 'trigger',
    quotes: 'quote',
    counters: 'counter'
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

  // Reset the checkbox map whenever a new preview lands: everything checked
  // except items flagged with an error-severity diagnostic.
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
    if (s.counters) parts.push(t('import.statCounters', { n: s.counters }));
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
    source ? SOURCE_LABEL[source] : t('import.railPickPending'),
    source === 'streamelements'
      ? credential
        ? t('import.railTokenSet')
        : t('import.railTokenPending')
      : uploadFile
        ? uploadFile.name
        : t('import.railFilePending'),
    previewResult ? selectionLine : t('import.railReviewPending'),
    commitResult ? t('import.railDone') : ''
  ]);

  // Count tiles on the done panel — only collections that actually landed.
  const appliedTiles = $derived.by(() => {
    const a = commitResult?.applied;
    if (!a) return [] as { n: number; label: string }[];
    const out: { n: number; label: string }[] = [];
    if (a.commands) out.push({ n: a.commands, label: t('import.hCommands') });
    if (a.timers) out.push({ n: a.timers, label: t('import.hTimers') });
    if (a.triggers) out.push({ n: a.triggers, label: t('import.hTriggers') });
    if (a.quotes) out.push({ n: a.quotes, label: t('import.hQuotes') });
    if (a.counters) out.push({ n: a.counters, label: t('import.hCounters') });
    return out;
  });

  const reviewHint = $derived.by(() => {
    if (!source) return '';
    let s = t('import.reviewHint', {
      source: SOURCE_LABEL[source as ImportSource],
      stats: statsLine
    });
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
    out.counters = keep(m.counters as ManifestCounter[] | undefined, 'counters');
    // Automod terms have no row of their own, so they used to ride along even
    // when every row was unchecked: the commit bar could read "0 of N selected"
    // and the commit would still call commitAutomodTerms. Nothing selected now
    // means nothing imported.
    if (m.automod && rowPicked > 0) out.automod = m.automod;
    return JSON.stringify(out);
  }

  // --- form handlers -------------------------------------------------------
  // Client ceilings mirror/precede the server's (+page.server.ts): Moobot
  // JSON is parsed in the browser and capped at 10MB before it is even read;
  // StreamLabs .db still uploads whole (20MB) because console CSP forbids
  // WASM — no 'wasm-unsafe-eval' in script-src (console/shared/svelte-
  // config.js), so a browser-side SQLite reader is a no-go. Decision record:
  // adding the directive to loosen CSP was weighed and rejected; one source
  // keeping its server path costs less than widening script-src for every
  // dashboard visitor.
  const MAX_JSON_BYTES = 10 * 1024 * 1024;
  const MAX_UPLOAD_BYTES = 20 * 1024 * 1024;

  // Instructions content per source: numbered steps are list leaves; the
  // StreamElements JWT hunt additionally gets a real deep link.
  const INSTR_KEY: Partial<
    Record<
      ImportSource,
      'import.instrSe' | 'import.instrMoobot' | 'import.instrNightbot' | 'import.instrSl'
    >
  > = {
    streamelements: 'import.instrSe',
    moobot: 'import.instrMoobot',
    nightbot: 'import.instrNightbot',
    streamlabs_desktop: 'import.instrSl'
  };
  const instrSteps = $derived.by(() => {
    if (!source) return [] as string[];
    const key = INSTR_KEY[source];
    return key ? tl(key) : [];
  });

  let previewError = $state('');
  let commitError = $state('');

  async function runPreview() {
    if (!source || submitting) return;
    previewError = '';
    if (source === 'fossabot') {
      previewError = t('import.errFossabot');
      return;
    }

    const body = new FormData();
    body.set('source', source);

    if (source === 'streamelements') {
      if (!credential.trim()) {
        previewError = t('import.errJwtMissing');
        return;
      }
      body.set('credential', credential.trim());
    } else if (!uploadFile || uploadFile.size === 0) {
      previewError = t('import.errFileMissing');
      return;
    } else if (isClientParsed(source)) {
      if (uploadFile.size > MAX_JSON_BYTES) {
        previewError = t('import.errTooLarge', { limit: 10 });
        return;
      }
      // Parse locally: JSON.parse inside the parser (never eval), with
      // per-item degradation. Only the resulting manifest rides to the server,
      // which re-validates it through validateManifest for authoritative
      // diagnostics/collisions/stats.
      submitting = true;
      try {
        const bytes = new Uint8Array(await uploadFile.arrayBuffer());
        const parsed = CLIENT_PARSED[source](bytes);
        const capped = applyImportCaps(parsed.manifest);
        body.set('manifest', JSON.stringify(capped.manifest));
        const r = await postPreview(body);
        if (r.ok && r.preview) {
          // Caps fired client-side only (the overflow never reached the
          // server); keep those warnings visible alongside the server's.
          r.preview.diagnostics = [...capped.diagnostics, ...(r.preview.diagnostics ?? [])];
          previewResult = r.preview;
          step = 'review';
        } else {
          previewError = r.error || t('import.errGeneric');
        }
      } catch (err) {
        if (err instanceof MoobotExportError || err instanceof NightbotExportError)
          previewError = t('import.errParseFailed', {
            m: err.message.replace(/^importer\/(?:moobot|nightbot):\s*/, '')
          });
        else previewError = t('import.errGeneric');
      }
      submitting = false;
      return;
    } else {
      // streamlabs_desktop: binary upload via the server path (CSP no-go for
      // client WASM — see the decision record above).
      if (uploadFile.size > MAX_UPLOAD_BYTES) {
        previewError = t('import.errTooLarge', { limit: 20 });
        return;
      }
      body.set('file', uploadFile);
    }

    submitting = true;
    const r = await postPreview(body);
    if (r.ok && r.preview) {
      previewResult = r.preview;
      step = 'review';
    } else {
      previewError = r.error || t('import.errGeneric');
    }
    submitting = false;
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
        commitError = d?.error || t('import.errGeneric');
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

  // Courtesy gate at drop time: fail fast on an obviously wrong file type so
  // the user gets a clear message instead of a parse failure after submit.
  // NOT a security control — extensions are trivially spoofed either way; the
  // authoritative checks stay content-based (JSON envelope shape for Moobot,
  // SQLite magic bytes + feature-table probe for StreamLabs).
  const WANT_EXT: Partial<Record<ImportSource, string>> = {
    moobot: '.json',
    nightbot: '.json',
    streamlabs_desktop: '.db'
  };

  function pickFile(f: File | null | undefined) {
    previewError = '';
    if (!f) {
      uploadFile = null;
      return;
    }
    const want = source ? WANT_EXT[source] : undefined;
    if (want && !f.name.toLowerCase().endsWith(want)) {
      previewError = t('import.errWrongType', { want });
      uploadFile = null;
      return;
    }
    uploadFile = f;
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('settings.eyebrow')} description={t('import.pageDesc')}>
    {t('import.pageTitlePre')}{' '}<em>{t('import.pageTitleEm')}</em>
  </PageHead>

  <div class="wizard">
    <!-- Persistent progress rail: stage list plus what has actually been
         chosen so far. Sticky, so the flow column scrolls under it. -->
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
      <h2>{t('import.stepPick')}</h2>
      <p class="hint">{t('import.pickHint')}</p>

      <div class="tiles">
        <!-- StreamElements: API-backed, needs the channel JWT. The input for
             it lives on the next (instructions) step, so tiles stay pure
             selectors — picking one advances immediately. -->
        <label class="tile" class:picked={source === 'streamelements'} data-cursor>
          <input
            type="radio"
            name="source-pick"
            value="streamelements"
            checked={source === 'streamelements'}
            onchange={() => choose('streamelements')}
          />
          <span class="tile-top">
            <span class="glyph" aria-hidden="true">{SOURCE_INITIALS.streamelements}</span>
            <span class="tile-name">StreamElements</span>
            <span class="chip">{t('import.chipToken')}</span>
          </span>
          <span class="tile-desc">{t('import.seDesc')}</span>
          <span class="tile-cta">{t('import.tileCta')}</span>
        </label>

        <!-- Fossabot: the parser exists backend-side but is not registered yet
             and its OAuth connect flow is unbuilt, so the tile ships visibly
             disabled rather than half-working. -->
        <div class="tile disabled" aria-disabled="true">
          <span class="tile-top">
            <span class="glyph" aria-hidden="true">{SOURCE_INITIALS.fossabot}</span>
            <span class="tile-name">Fossabot</span>
            <span class="chip soon">{t('import.chipSoon')}</span>
          </span>
          <span class="tile-desc">{t('import.fossabotDesc')}</span>
        </div>

        <!-- Moobot: file export (.json), parsed in the browser -->
        <label class="tile" class:picked={source === 'moobot'} data-cursor>
          <input
            type="radio"
            name="source-pick"
            value="moobot"
            checked={source === 'moobot'}
            onchange={() => choose('moobot')}
          />
          <span class="tile-top">
            <span class="glyph" aria-hidden="true">{SOURCE_INITIALS.moobot}</span>
            <span class="tile-name">Moobot</span>
            <span class="chip">{t('import.chipFile')}</span>
          </span>
          <span class="tile-desc">{t('import.moobotDesc')}</span>
          <span class="tile-cta">{t('import.tileCta')}</span>
        </label>

        <!-- Nightbot: saved API JSON (commands/timers/spam protection),
             parsed in the browser like Moobot's export -->
        <label class="tile" class:picked={source === 'nightbot'} data-cursor>
          <input
            type="radio"
            name="source-pick"
            value="nightbot"
            checked={source === 'nightbot'}
            onchange={() => choose('nightbot')}
          />
          <span class="tile-top">
            <span class="glyph" aria-hidden="true">{SOURCE_INITIALS.nightbot}</span>
            <span class="tile-name">Nightbot</span>
            <span class="chip">{t('import.chipFile')}</span>
          </span>
          <span class="tile-desc">{t('import.nightbotDesc')}</span>
          <span class="tile-cta">{t('import.tileCta')}</span>
        </label>

        <!-- StreamLabs Chatbot: desktop database export (.db) -->
        <label class="tile" class:picked={source === 'streamlabs_desktop'} data-cursor>
          <input
            type="radio"
            name="source-pick"
            value="streamlabs_desktop"
            checked={source === 'streamlabs_desktop'}
            onchange={() => choose('streamlabs_desktop')}
          />
          <span class="tile-top">
            <span class="glyph" aria-hidden="true">{SOURCE_INITIALS.streamlabs_desktop}</span>
            <span class="tile-name">StreamLabs Chatbot</span>
            <span class="chip">{t('import.chipFile')}</span>
          </span>
          <span class="tile-desc">{t('import.slDesc')}</span>
          <span class="tile-cta">{t('import.tileCta')}</span>
        </label>
      </div>
    </Card>
  {:else if step === 'instructions' && source}
    <Card>
      <div class="instr-head">
        <span class="glyph" aria-hidden="true">{SOURCE_INITIALS[source]}</span>
        <h2>{t('import.stepInstructions', { source: SOURCE_LABEL[source] })}</h2>
      </div>
      <p class="hint">{t('import.instrHint', { source: SOURCE_LABEL[source] })}</p>

      {#if instrSteps.length}
        <ol class="steps">
          {#each instrSteps as s, i (i)}<li>{s}</li>{/each}
        </ol>
      {/if}

      {#if source === 'streamelements'}
        <p class="instr-link">
          <a
            href="https://streamelements.com/dashboard/account/channels"
            target="_blank"
            rel="noopener noreferrer">{t('import.seLinkLabel')}</a
          >
        </p>
        <div class="cred">
          <textarea
            rows="3"
            placeholder="eyJhbGciOi…"
            bind:value={credential}
            spellcheck="false"
            autocomplete="off"
            autocapitalize="off"
            aria-label={t('import.jwtFieldAria')}
          ></textarea>
          <p class="hint">
            {@html t('import.jwtHint')}
          </p>
        </div>
      {:else if source === 'moobot' || source === 'nightbot' || source === 'streamlabs_desktop'}
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
            accept={source === 'streamlabs_desktop'
              ? '.db,application/octet-stream'
              : '.json,application/json'}
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

      {#if previewError}<AlertBanner icon="ban">{previewError}</AlertBanner>{/if}

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
          <Button type="submit" variant="primary" icon="send" loading={submitting}>
            {t('import.continueCta')}
          </Button>
        </div>
      </form>
    </Card>
  {:else if step === 'review' && previewResult?.manifest}
    <Card class="review-head">
      <h2>{t('import.reviewTitle')}</h2>
      <p class="hint">{reviewHint}</p>

      <div class="review-bar">
        {#each statChips as c (c)}<span class="stat">{c}</span>{/each}
        <span class="review-spacer"></span>
        <button type="button" class="mini" onclick={() => setAll(true)}>{t('import.selectAll')}</button>
        <button type="button" class="mini" onclick={() => setAll(false)}>{t('import.selectNone')}</button>
      </div>
    </Card>

      {#each manifestLevelDiags as d (d.code + d.message)}
        <p class="manifest-warn" role="status">{d.message}</p>
      {/each}

      {#if anyCollisions}
        <div class="collision-note">
          {@html t('import.conflictsNote', { n: previewResult.collisions?.length ?? 0 })}
          <label class="overwrite-toggle">
            <input type="checkbox" bind:checked={overwrite} name="overwrite" value="on" />
            {t('import.overwriteToggle')}
          </label>
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
                <label class="pick">
                  <input
                    type="checkbox"
                    checked={isChecked('commands', i)}
                    onchange={(e) => toggle('commands', i, e.currentTarget.checked)}
                  />
                  <span class="row-name">!{c.name}</span>
                </label>
                <div class="row-body">
                  <span class="row-response">{c.responses?.join(' / ')}</span>
                  <span class="chips">
                    {#if c.permission && c.permission !== 'everyone'}<Badge perm={c.permission} />{/if}
                    {#if c.cooldown_seconds}<span class="chip">{t('import.cooldownChip', { n: c.cooldown_seconds })}</span>{/if}
                    {#each c.aliases ?? [] as a (a)}<span class="alias-chip">!{a}</span>{/each}
                    {#each diags.filter((d) => d.severity === 'warn') as d (d.code + d.message)}
                      <span class="warn-chip" title={d.message}>{d.message}</span>
                    {/each}
                    {#each diags.filter((d) => d.severity === 'error') as d (d.code + d.message)}
                      <span class="error-chip" title={d.message}>{t('import.cannotImport', { m: d.message })}</span>
                    {/each}
                    {#if collidedCommands.has(normalizeName(c.name))}
                      <span class="collision-chip">{t('import.alreadyExists')}</span>
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
                <label class="pick">
                  <input
                    type="checkbox"
                    checked={isChecked('timers', i)}
                    onchange={(e) => toggle('timers', i, e.currentTarget.checked)}
                  />
                </label>
                <div class="row-body">
                  <span class="row-response">{tm.message}</span>
                  <span class="chips">
                    <span class="chip">{t('import.everySeconds', { n: tm.interval_seconds })}</span>
                    {#each diags.filter((d) => d.severity === 'warn') as d (d.code + d.message)}
                      <span class="warn-chip" title={d.message}>{d.message}</span>
                    {/each}
                    {#each diags.filter((d) => d.severity === 'error') as d (d.code + d.message)}
                      <span class="error-chip" title={d.message}>{t('import.cannotImport', { m: d.message })}</span>
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
                <label class="pick">
                  <input
                    type="checkbox"
                    checked={isChecked('triggers', i)}
                    onchange={(e) => toggle('triggers', i, e.currentTarget.checked)}
                  />
                  <span class="row-name">{tg.phrase}</span>
                </label>
                <div class="row-body">
                  <span class="row-response">{tg.response}</span>
                  <span class="chips">
                    {#each diags.filter((d) => d.severity === 'warn') as d (d.code + d.message)}
                      <span class="warn-chip" title={d.message}>{d.message}</span>
                    {/each}
                    {#each diags.filter((d) => d.severity === 'error') as d (d.code + d.message)}
                      <span class="error-chip" title={d.message}>{t('import.cannotImport', { m: d.message })}</span>
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

      {#if previewResult.manifest.counters?.length}
        <Card class="group">
          <div class="group-head">
            <span class="group-title">{t('import.hCounters')}</span>
            <span class="group-count">{previewResult.manifest.counters.length}</span>
          </div>
          <ul class="rows">
            {#each previewResult.manifest.counters as ctr, i (ctr.name)}
              {@const diags = itemDiags('counters', i)}
              <li class="row-item">
                <label class="pick">
                  <input
                    type="checkbox"
                    checked={isChecked('counters', i)}
                    onchange={(e) => toggle('counters', i, e.currentTarget.checked)}
                  />
                  <span class="row-name">{`{counter:` + ctr.name + `}`}</span>
                </label>
                <div class="row-body">
                  <span class="row-response">{t('import.startsAt', { n: ctr.value })}</span>
                  <span class="chips">
                    {#each diags.filter((d) => d.severity === 'warn') as d (d.code + d.message)}
                      <span class="warn-chip" title={d.message}>{d.message}</span>
                    {/each}
                    {#each diags.filter((d) => d.severity === 'error') as d (d.code + d.message)}
                      <span class="error-chip" title={d.message}>{t('import.cannotImport', { m: d.message })}</span>
                    {/each}
                  </span>
                </div>
              </li>
            {/each}
          </ul>
        </Card>
      {/if}

      {#if commitError}<AlertBanner icon="ban">{commitError}</AlertBanner>{/if}

      <!-- Sticky commit bar: the selection count travels with the list so the
           import button is never scrolled off behind a long review. -->
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
          <Button type="submit" variant="primary" icon="check" loading={submitting}>
            {t('import.importNow')}
          </Button>
        </div>
      </form>
  {:else if step === 'done'}
    <Card class="done-panel">
      <!-- The blob is seeded off the channel name, so the face that congratulates
           you here is the same one the topbar has been wearing all session. -->
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
      <h2>{t('import.doneTitle')}</h2>
      {#if commitResult}
        <p class="hint">
          {t('import.doneLine', {
            c: commitResult.applied.commands,
            tm: commitResult.applied.timers,
            tg: commitResult.applied.triggers,
            q: commitResult.applied.quotes,
            ct: commitResult.applied.counters
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
        <Button variant="primary" icon="check" onclick={reset}>{t('import.backToSources')}</Button>
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
  h2 {
    margin: 0 0 6px;
    font-size: 16px;
  }
  .hint {
    color: var(--bb-muted, #888077);
    font-size: 13px;
    margin: 0 0 12px;
  }

  /* --- wizard shell: sticky progress rail + flow column --- */
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

  /* Below the two-column breakpoint the rail stops being a sidebar: it goes
     back to normal flow above the steps rather than eating a scroll-locked
     column on a phone. */
  @media (max-width: 900px) {
    .wizard {
      grid-template-columns: minmax(0, 1fr);
    }
    :global(.rail) {
      position: static;
    }
  }

  /* --- step 1: source tiles --- */  .tiles {
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
    border-radius: 10px;
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
    gap: 10px;
  }
  .glyph {
    flex: none;
    width: 34px;
    height: 34px;
    border-radius: 10px;
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
  .chip {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--bb-tan-light);
    border: 1px solid rgba(201, 168, 124, 0.3);
    border-radius: var(--bb-radius-pill);
    padding: 3px 9px;
    white-space: nowrap;
  }
  .chip.soon {
    color: var(--bb-muted);
    border-color: var(--glass-border);
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
  .instr-link a {
    color: var(--bb-tan-light);
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .instr-link a:hover {
    color: var(--bb-tan);
  }
  .cred {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-bottom: 6px;
  }
  .cred textarea {
    width: 100%;
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--glass-border);
    border-radius: 6px;
    color: var(--bb-white);
    padding: 8px 10px;
    font-size: 13px;
    font-family: var(--bb-font-mono);
    resize: vertical;
  }
  .drop {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 64px;
    padding: 10px 34px;
    border: 1px dashed var(--glass-border);
    border-radius: 8px;
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
  .stat {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.06em;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.08);
    border: 1px solid rgba(201, 168, 124, 0.28);
    border-radius: var(--bb-radius-pill);
    padding: 5px 12px;
    white-space: nowrap;
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
    border-radius: 8px;
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
    cursor: pointer;
    white-space: nowrap;
  }
  .pick input {
    accent-color: var(--bb-tan, #c9a87c);
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
  .alias-chip,
  .collision-chip {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-pill);
    padding: 2px 8px;
  }
  .collision-chip {
    color: #e5484d;
    border-color: rgba(229, 72, 77, 0.4);
  }
  .warn-chip {
    font-size: 11px;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.1);
    border: 1px solid rgba(201, 168, 124, 0.28);
    border-radius: var(--bb-radius-pill);
    padding: 2px 8px;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .error-chip {
    font-size: 11px;
    color: #e5484d;
    background: rgba(229, 72, 77, 0.08);
    border: 1px solid rgba(229, 72, 77, 0.35);
    border-radius: var(--bb-radius-pill);
    padding: 2px 8px;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .collision-note {
    border: 1px solid rgba(229, 72, 77, 0.35);
    background: rgba(229, 72, 77, 0.06);
    border-radius: 8px;
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
    cursor: pointer;
    font-size: 13px;
    white-space: nowrap;
  }
  .overwrite-toggle input {
    accent-color: var(--bb-tan, #c9a87c);
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
  .instr-head h2 {
    margin: 0;
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
  .mini {
    font: inherit;
    cursor: pointer;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-muted);
    background: transparent;
    border: 1px solid var(--bb-border);
    border-radius: 999px;
    padding: 6px 13px;
    transition:
      color var(--bb-dur-fast, 140ms) ease,
      background var(--bb-dur-fast, 140ms) ease;
  }
  .mini:hover {
    color: var(--bb-white);
    background: rgba(201, 168, 124, 0.08);
  }

  /* Each collection is its own panel, so a long commands list cannot push the
     counters heading out of sight of its own rows. */
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
    border-radius: 999px;
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
    border-radius: 16px;
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
    border-radius: 8px;
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
      border-radius: 8px;
      padding: 14px 16px;
    }
  }
</style>
