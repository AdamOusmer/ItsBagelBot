// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { toast } from '@bagel/ui/svelte/toast';
import { applyImportCaps } from '@bagel/kit/importer/caps';
import {
  IMPORT_STRATEGIES,
  type FileInputSpec,
  type InputSpec,
  type OAuthInputSpec,
  type TextInputSpec
} from '@bagel/kit/importer/strategy';
import type { CommitResponse, ImportDiagnostic, ImportSource, PreviewResponse } from '@bagel/kit';
import { localizeImporterError } from '#lib/importer-errors';
import { failureKey, type ImportFailure } from './errors';
import {
  buildRetryManifest,
  buildSelectedManifest,
  buildSelection,
  countFatal,
  countPicked,
  countRows,
  diagsAt,
  indexDiagnostics,
  isChecked,
  mergeRetry,
  retryable,
  type RowKind,
  type Selection
} from './helpers';
import { postImportAction, type ActionOutcome } from './request';

export type ImportTranslate = (text: string, params?: Record<string, string | number>) => string;
export type RunResult = 'ok' | 'failed' | 'cancelled' | 'idle';

export interface ImportSessionEnv {
  t: ImportTranslate;
  isConnected: () => boolean;
  onPosting?: () => void;
}

type PreparedInput = { error: string } | { diags: ImportDiagnostic[] };

export interface ImportSnapshot {
  source: ImportSource | '';
  stage: string;
  overwrite: boolean;
  previewResult: PreviewResponse | null;
  commitResult: CommitResponse | null;
  selected: Selection;
}

const identity: ImportTranslate = (text) => text;

export class ImportSession {
  source = $state<ImportSource | ''>('');
  stage = $state('pick');
  credential = $state('');
  uploadFile = $state<File | null>(null);
  submitting = $state(false);
  previewResult = $state<PreviewResponse | null>(null);
  commitResult = $state<CommitResponse | null>(null);
  selected = $state<Selection>({});
  overwrite = $state(false);
  previewError = $state('');
  commitError = $state('');

  private controller: AbortController | null = null;
  private env: ImportSessionEnv = { t: identity, isConnected: () => false };

  configure(env: ImportSessionEnv) {
    this.env = env;
  }

  private t: ImportTranslate = (text, params) => this.env.t(text, params);

  diagIndex = $derived(indexDiagnostics(this.previewResult?.diagnostics));
  diagsFor = (kind: RowKind, i: number) => diagsAt(this.diagIndex, kind, i);
  isPicked = (kind: RowKind, i: number) => isChecked(this.selected, kind, i);

  fatalCount = $derived(countFatal(this.previewResult));
  rowTotal = $derived(countRows(this.previewResult?.manifest));
  rowPicked = $derived(countPicked(this.previewResult?.manifest, this.selected));
  anyCollisions = $derived((this.previewResult?.collisions ?? []).length > 0);
  collidedCommands = $derived(
    new Set((this.previewResult?.collisions ?? []).filter((c) => c.kind === 'command').map((c) => c.name))
  );
  manifestLevelDiags = $derived((this.previewResult?.diagnostics ?? []).filter((d) => d.item_index < 0));

  statChips = $derived.by(() => {
    const s = this.previewResult?.stats;
    if (!s) return [] as string[];
    const parts: string[] = [];
    if (s.commands) parts.push(this.t('statCommands', { n: s.commands }));
    if (s.timers) parts.push(this.t('statTimers', { n: s.timers }));
    if (s.triggers) parts.push(this.t('statTriggers', { n: s.triggers }));
    if (s.quotes) parts.push(this.t('statQuotes', { n: s.quotes }));
    return parts;
  });
  statsLine = $derived(this.statChips.join(' · ') || this.t('statsNone'));
  selectionLine = $derived(this.t('selectionLine', { n: this.rowPicked, total: this.rowTotal }));

  appliedTiles = $derived.by(() => {
    const a = this.commitResult?.applied;
    if (!a) return [] as { n: number; label: string }[];
    const out: { n: number; label: string }[] = [];
    if (a.commands) out.push({ n: a.commands, label: this.t('hCommands') });
    if (a.timers) out.push({ n: a.timers, label: this.t('hTimers') });
    if (a.triggers) out.push({ n: a.triggers, label: this.t('hTriggers') });
    if (a.quotes) out.push({ n: a.quotes, label: this.t('hQuotes') });
    return out;
  });

  retryCount = $derived(retryable(this.commitResult?.failed).length);

  hasProgress = $derived(!!this.previewResult || !!this.commitResult);

  inputDetail(spec: InputSpec | null): string {
    if (spec?.kind === 'text') return this.textInputDetail(spec);
    return this.uploadFile ? this.uploadFile.name : this.t('railFilePending');
  }

  private textInputDetail(spec: TextInputSpec): string {
    if (spec.secret) return this.credential ? this.t('railTokenSet') : this.t('railTokenPending');
    return this.credential ? this.t('railHandleSet') : this.t('railHandlePending');
  }

  toggle(kind: RowKind, i: number, checked: boolean) {
    this.selected = { ...this.selected, [`${kind}:${i}`]: checked };
  }

  setAll(want: boolean) {
    this.selected = buildSelection(this.previewResult, want);
  }

  choose(s: ImportSource) {
    this.source = s;
    this.uploadFile = null;
    this.credential = '';
  }

  reset() {
    this.cancel();
    this.source = '';
    this.stage = 'pick';
    this.uploadFile = null;
    this.credential = '';
    this.overwrite = false;
    this.previewResult = null;
    this.commitResult = null;
    this.selected = {};
    this.previewError = '';
    this.commitError = '';
  }

  cancel() {
    this.controller?.abort();
  }

  pickFile(f: File | null | undefined, inputSpec: InputSpec | null): 'picked' | 'cleared' | 'rejected' {
    this.previewError = '';
    if (!f) {
      this.uploadFile = null;
      return 'cleared';
    }
    const want = wantedExtension(inputSpec);
    if (want && !f.name.toLowerCase().endsWith(want)) {
      this.previewError = this.t('errWrongType', { want });
      this.uploadFile = null;
      return 'rejected';
    }
    this.uploadFile = f;
    return 'picked';
  }

  snapshot(): ImportSnapshot {
    return {
      source: this.source,
      stage: this.stage,
      overwrite: this.overwrite,
      previewResult: this.previewResult,
      commitResult: this.commitResult,
      selected: this.selected
    };
  }

  restore(snap: ImportSnapshot) {
    this.source = snap.source;
    this.stage = snap.stage;
    this.overwrite = snap.overwrite;
    this.previewResult = snap.previewResult;
    this.commitResult = snap.commitResult;
    this.selected = snap.selected;
  }

  private failureMessage(failure: ImportFailure, message?: string): string {
    if (failure !== 'generic') return this.t(failureKey(failure));
    return localizeImporterError(message, this.t) || this.t(failureKey('generic'));
  }

  private async post(action: 'preview' | 'commit', body: FormData): Promise<ActionOutcome> {
    this.controller = new AbortController();
    try {
      return await postImportAction(action, body, this.controller);
    } finally {
      this.controller = null;
    }
  }

  async runPreview(): Promise<RunResult> {
    const source = this.source;
    if (!source || this.submitting) return 'idle';
    this.previewError = '';
    this.submitting = true;
    try {
      return await this.previewFlow(source);
    } finally {
      this.submitting = false;
    }
  }

  private async previewFlow(source: ImportSource): Promise<RunResult> {
    const body = new FormData();
    body.set('source', source);
    const prepared = await this.prepareInput(IMPORT_STRATEGIES[source].input, body);
    if ('error' in prepared) {
      this.previewError = prepared.error;
      return 'failed';
    }
    this.env.onPosting?.();
    const outcome = await this.post('preview', body);
    if (outcome.kind === 'cancelled') return 'cancelled';
    const preview = outcome.kind === 'success' ? readPreview(outcome.data) : null;
    if (!preview) {
      this.previewError = this.outcomeMessage(outcome);
      return 'failed';
    }
    preview.diagnostics = [...prepared.diags, ...(preview.diagnostics ?? [])];
    this.previewResult = preview;
    this.selected = buildSelection(preview, true);
    return 'ok';
  }

  private outcomeMessage(outcome: ActionOutcome): string {
    if (outcome.kind === 'failure') return this.failureMessage(outcome.failure, outcome.message);
    return this.t(failureKey('generic'));
  }

  async runCommit(): Promise<RunResult> {
    if (this.submitting) return 'idle';
    this.commitError = '';
    const manifestJson = buildSelectedManifest(this.previewResult, this.selected);
    if (manifestJson === '{}') {
      this.commitError = this.t('errNothingSelected');
      return 'failed';
    }
    this.submitting = true;
    try {
      return await this.commitFlow(manifestJson);
    } finally {
      this.submitting = false;
    }
  }

  async runRetry(): Promise<RunResult> {
    if (this.submitting || !this.commitResult) return 'idle';
    this.commitError = '';
    const manifestJson = buildRetryManifest(
      buildSelectedManifest(this.previewResult, this.selected),
      this.commitResult.failed
    );
    if (manifestJson === '{}') {
      this.commitError = this.t('errNothingSelected');
      return 'failed';
    }
    this.submitting = true;
    try {
      return await this.commitFlow(manifestJson, this.commitResult);
    } finally {
      this.submitting = false;
    }
  }

  private async commitFlow(manifestJson: string, previous?: CommitResponse): Promise<RunResult> {
    const body = new FormData();
    body.set('manifest', manifestJson);
    body.set('source', this.source);
    if (this.overwrite) body.set('overwrite', 'on');
    const outcome = await this.post('commit', body);
    if (outcome.kind === 'cancelled') return 'cancelled';
    const commit = outcome.kind === 'success' ? readCommit(outcome.data) : null;
    if (!commit) {
      this.commitError = this.outcomeMessage(outcome);
      return 'failed';
    }
    this.commitResult = previous ? mergeRetry(previous, commit) : commit;
    toast('success', this.t(previous ? 'toastRetried' : 'toastApplied'));
    return 'ok';
  }

  private prepareInput(spec: InputSpec, body: FormData): Promise<PreparedInput> | PreparedInput {
    if (spec.kind === 'text') return this.prepareText(spec, body);
    if (spec.kind === 'oauth') return this.prepareOauth(spec);
    return this.prepareFile(spec, body);
  }

  private prepareText(spec: TextInputSpec, body: FormData): PreparedInput {
    const value = this.credential.trim();
    if (value === '') return { error: this.t(spec.i18n.errMissing) };
    if (value.length > spec.maxLen || !spec.shape.test(value)) return { error: this.t(spec.i18n.errShape) };
    body.set('credential', value);
    return { diags: [] };
  }

  private prepareOauth(spec: OAuthInputSpec): PreparedInput {
    if (!this.env.isConnected()) return { error: this.t(spec.i18n.errNotConnected) };
    return { diags: [] };
  }

  private async prepareFile(spec: FileInputSpec, body: FormData): Promise<PreparedInput> {
    const file = this.uploadFile;
    if (!file || file.size === 0) return { error: this.t('errFileMissing') };
    if (file.size > spec.maxBytes)
      return { error: this.t('errTooLarge', { limit: Math.round(spec.maxBytes / (1024 * 1024)) }) };
    if (!spec.parseInBrowser) {
      body.set('file', file);
      return { diags: [] };
    }
    return this.parseInPage(spec.parseInBrowser, file, body);
  }

  private async parseInPage(
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
      return { error: this.parseErrorMessage(err) };
    }
  }

  private parseErrorMessage(err: unknown): string {
    const message = err instanceof Error ? err.message : '';
    const parserRefusal = /^importer\/[a-z-]+:\s*([\s\S]*)$/.exec(message);
    if (!parserRefusal) return this.t('errGeneric');
    return this.t('errParseFailed', { m: parserRefusal[1] });
  }
}

function readPreview(data: Record<string, unknown>): PreviewResponse | null {
  const preview = data.preview as PreviewResponse | undefined;
  return data.ok && preview?.manifest ? preview : null;
}

function readCommit(data: Record<string, unknown>): CommitResponse | null {
  const commit = data.commit as CommitResponse | undefined;
  return data.ok && commit ? commit : null;
}

export function wantedExtension(spec: InputSpec | null): string {
  if (spec?.kind !== 'file') return '';
  const first = spec.accept.split(',')[0];
  return first.startsWith('.') ? first : '';
}
