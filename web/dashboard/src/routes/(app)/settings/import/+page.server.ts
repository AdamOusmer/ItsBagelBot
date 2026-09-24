// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { dev } from '$app/environment';
import { fail, redirect } from '@sveltejs/kit';
import type { Cookies } from '@sveltejs/kit';
import { previewImport, commitImport, SERVER_STRATEGIES } from '$lib/server/importer';
import type { SourceInput } from '$lib/server/importer';
import { IMPORT_STRATEGIES, isImportSource } from '@bagel/kit/importer/strategy';
import { ValkeyRateLimiter } from '@bagel/kit/server/rate-limit';
import { actionError } from '$lib/server/action-errors';
import type { Session } from '$lib/server/session';
import {
  IMPORT_SOURCES,
  translate,
  type Locale,
  type CommitResponse,
  type ImportManifest,
  type ImportSource,
  type PreviewResponse
} from '@bagel/kit';

const MAX_UPLOAD_BYTES = 20 * 1024 * 1024;

const MAX_MANIFEST_JSON_BYTES = 8 * 1024 * 1024;

const importLimiter = new ValkeyRateLimiter({ name: 'import', capacity: 10, refillPerSec: 10 / 60 });

async function importAllowed(s: Session): Promise<boolean> {
  const decision = await importLimiter.check(`import:${s.user_id}`);
  return decision.allowed;
}

const DEMO = dev && process.env.DEMO === '1';

type GateVerdict = { ok: true; session: Session } | { ok: false; status: number; error: string };

async function importGate(locals: App.Locals): Promise<GateVerdict> {
  const s = await requireOwner(locals);
  if (!s) return { ok: false, status: 403, error: actionError(locals.locale, 'Not allowed.') };
  if (!(await importAllowed(s)))
    return { ok: false, status: 429, error: actionError(locals.locale, 'Too many import attempts. Wait a minute and try again.') };
  return { ok: true, session: s };
}

async function requireOwner(locals: App.Locals): Promise<Session | null> {
  if (DEMO) {
    const { demoSession } = await import('$lib/server/demo-data');
    return demoSession();
  }
  const s = locals.session;
  if (!s || s.delegate_of) return null;
  return s;
}

export const load: PageServerLoad = async ({ locals, cookies }) => {
  const s = await requireOwner(locals);
  if (!s) throw redirect(302, '/');
  return { connected: connectedSources(cookies) };
};

function connectedSources(cookies: Cookies): Record<ImportSource, boolean> {
  const out = {} as Record<ImportSource, boolean>;
  for (const id of IMPORT_SOURCES) out[id] = sourceConnected(id, cookies);
  return out;
}

function sourceConnected(id: ImportSource, cookies: Cookies): boolean {
  const isConnected = SERVER_STRATEGIES[id].connected;
  if (!isConnected) return false;
  return DEMO || isConnected(cookies);
}

function decodePreManifest(form: FormData):
  | { ok: true; manifest?: ImportManifest }
  | { ok: false; status: number; error: string } {
  const rawManifest = String(form.get('manifest') ?? '');
  if (rawManifest === '') return { ok: true };
  if (rawManifest.length > MAX_MANIFEST_JSON_BYTES)
    return { ok: false, status: 400, error: 'The parsed import is too large to verify.' };
  try {
    const parsed = JSON.parse(rawManifest) as ImportManifest;
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
      throw new Error('not an object');
    return { ok: true, manifest: parsed };
  } catch {
    return { ok: false, status: 400, error: 'The parsed import could not be decoded. Run the preview again.' };
  }
}

async function readUpload(form: FormData): Promise<
  { ok: true; fileB64: string; uploaded: boolean } | { ok: false; status: number; error: string }
> {
  const upload = form.get('file');
  if (!(upload instanceof File) || upload.size === 0) return { ok: true, fileB64: '', uploaded: false };
  if (upload.size > MAX_UPLOAD_BYTES)
    return {
      ok: false,
      status: 400,
      error: 'That file is too large. Exported bot configs should be well under 20 MB.'
    };
  try {
    return {
      ok: true,
      fileB64: Buffer.from(await upload.arrayBuffer()).toString('base64'),
      uploaded: true
    };
  } catch {
    return { ok: false, status: 400, error: 'Could not read that file.' };
  }
}

async function readSourceInput(
  form: FormData,
  source: ImportSource
): Promise<{ ok: true; input: SourceInput } | { ok: false; status: number; error: string }> {
  const pre = decodePreManifest(form);
  if (!pre.ok) return { ok: false, status: pre.status, error: pre.error };

  const up = await readUpload(form);
  if (!up.ok) return { ok: false, status: up.status, error: up.error };

  const input: SourceInput = {
    preManifest: pre.manifest,
    fileB64: up.fileB64,
    credential: up.uploaded ? '' : String(form.get('credential') ?? '').trim()
  };
  const refusal = SERVER_STRATEGIES[source].acceptInput(input);
  if (refusal) return { ok: false, ...refusal };
  return { ok: true, input };
}

function usableSource(v: string, locale: Locale): ImportSource | { error: string } {
  if (!isImportSource(v)) return { error: actionError(locale, 'Pick a source to import from.') };
  const strategy = IMPORT_STRATEGIES[v];
  if (!strategy.available) return { error: translate(locale, 'serverErrors.importUnavailable', { source: strategy.label }) };
  return v;
}

function resolveCredential(source: ImportSource, input: SourceInput, cookies: Cookies): string | null {
  const strategy = SERVER_STRATEGIES[source];
  if (!strategy.credential) return input.credential;
  return strategy.credential(input, cookies);
}

export const actions: Actions = {
preview: async ({ request, locals, cookies }) => {
    const gate = await importGate(locals);
    if (!gate.ok) return fail(gate.status, { error: gate.error, step: 'preview' });

    const form = await request.formData();
    const source = usableSource(String(form.get('source') ?? ''), locals.locale);
    if (typeof source !== 'string') return fail(400, { error: source.error, step: 'preview' });

    const read = await readSourceInput(form, source);
    if (!read.ok) return fail(read.status, { error: actionError(locals.locale, read.error), step: 'preview' });

    if (DEMO) {
      const demo = await import('$lib/server/demo-import');
      return { ok: true, step: 'preview', source, preview: demo.demoImportPreview(source) };
    }

    const credential = resolveCredential(source, read.input, cookies);
    if (credential === null)
      return fail(400, {
        error: translate(locals.locale, 'serverErrors.importConnectFirst', { source: IMPORT_STRATEGIES[source].label }),
        step: 'preview'
      });

    let preview: PreviewResponse;
    try {
      preview = await previewImport(gate.session, {
        source,
        credential,
        file_b64: read.input.fileB64,
        manifest: read.input.preManifest
      });
    } catch {
      return fail(502, { error: actionError(locals.locale, 'The importer service did not answer. Try again in a moment.'), step: 'preview' });
    }

    if (!preview.manifest)
      return fail(422, {
        error: actionError(locals.locale, preview.error || 'Nothing could be imported from that source.'),
        step: 'preview'
      });

    return { ok: true, step: 'preview', source, preview };
  },

  commit: async ({ request, locals, cookies }) => {
    const gate = await importGate(locals);
    if (!gate.ok) return fail(gate.status, { error: gate.error, step: 'commit' });

    const form = await request.formData();
    const source = String(form.get('source') ?? '');
    const overwrite = form.get('overwrite') === 'on';
    const rawManifest = String(form.get('manifest') ?? '');
    if (!rawManifest) return fail(400, { error: actionError(locals.locale, 'Nothing selected to import.'), step: 'commit' });

    let manifest: ImportManifest;
    try {
      manifest = JSON.parse(rawManifest) as ImportManifest;
    } catch {
      return fail(400, { error: actionError(locals.locale, 'The selection could not be decoded. Run the preview again.'), step: 'commit' });
    }

    if (DEMO) {
      const demo = await import('$lib/server/demo-import');
      return { ok: true, step: 'commit', commit: demo.demoImportCommit(manifest, overwrite) };
    }

    let commit: CommitResponse;
    try {
      commit = await commitImport(gate.session, {
        source: isImportSource(source) ? source : '',
        manifest,
        overwrite
      });
    } catch {
      return fail(502, { error: actionError(locals.locale, 'The importer service did not answer. Try again in a moment.'), step: 'commit' });
    }

    if (commit.error)
      return fail(502, { error: commit.error, step: 'commit' });

    if (isImportSource(source)) SERVER_STRATEGIES[source].afterCommit?.(cookies);

    return { ok: true, step: 'commit', commit };
  }
};
