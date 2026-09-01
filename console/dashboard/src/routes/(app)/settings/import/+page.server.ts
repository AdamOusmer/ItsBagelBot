// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { dev } from '$app/environment';
import { fail, redirect } from '@sveltejs/kit';
import type { Cookies } from '@sveltejs/kit';
import { previewImport, commitImport } from '$lib/server/importer';
import { NB_COOKIE_PATH, NB_TOKEN_COOKIE } from '$lib/server/nightbot-oauth';
import { ValkeyRateLimiter } from '@bagel/shared/server/rate-limit';
import type { Session } from '$lib/server/session';
import type {
  CommitResponse,
  ImportManifest,
  ImportSource,
  PreviewResponse
} from '@bagel/shared';

// Upload ceiling for the file-based sources that still cross the wire.
// Since the Moobot path parses browser-side (+page.svelte), only StreamLabs
// .db uploads remain binary posts; 20MB covers those with room, and the
// client refuses past 10MB for Moobot JSON long before anything is read.
// The transport ceiling is adapter-node's BODY_SIZE_LIMIT env — raising it is
// a deploy-env change (deploy/k8s/console-dashboard.yaml), not a code one, so
// this check exists to fail with a readable message instead of a 413.
const MAX_UPLOAD_BYTES = 20 * 1024 * 1024;

// Ceiling on a pre-parsed manifest POST (Moobot path). The client truncates
// to IMPORT_ITEM_CAPS before sending (~1MB worst case at full caps), so this
// is a hostile-input backstop, not an expected shape.
const MAX_MANIFEST_JSON_BYTES = 8 * 1024 * 1024;

// Shape-check ceiling on a pasted StreamElements JWT (three dot-separated
// base64url segments, <=4KB), mirroring the shared parser's own gate.
const MAX_CREDENTIAL_LEN = 4096;

// Per-session budget just for preview/commit, tighter than hooks.server.ts's
// global write tier (30 burst / 0.5/s fleet-wide) which already applies to
// these actions as non-GET requests. A second, smaller bucket earns its keep
// because each import fans out into hundreds of cross-service writes plus an
// audit row — the one action type where a bored clicker costs real backend
// work, so 10 previews/minute sustained is plenty for a human mid-migration.
// Same Valkey-backed limiter, same failure posture: degraded per-pod bucket
// when Valkey is down, never a page taken down.
const importLimiter = new ValkeyRateLimiter({ name: 'import', capacity: 10, refillPerSec: 10 / 60 });

async function importAllowed(s: Session): Promise<boolean> {
  const decision = await importLimiter.check(`import:${s.user_id}`);
  return decision.allowed;
}

// DEMO=1 has no NATS mesh behind it, so the importer RPCs would just time out;
// preview/commit instead run against canned fixtures in demo-import.ts. Same
// gate shape as every other DEMO surface (dev && process.env.DEMO === '1'):
// `dev` is a build-time constant, so Rollup folds the branch and its dynamic
// import edge out of production builds entirely.
//
// Decision record — keep this const ABOVE every transitive reader (measured
// 2026-08-23): with importGate declared first, Rollup folded the initializer
// to false but stopped substituting references (requireOwner's dynamic
// demo-data import survived as live code behind a runtime flag) and the
// production-clean scan failed. Constant propagation is transitive through
// calls, so the declaring order below is load-bearing.
const DEMO = dev && process.env.DEMO === '1';

// GateVerdict collapses the two refusals every action shares into one value:
// each action spends a single branch on them. fail() stays at the call site so
// SvelteKit keeps inferring ActionData from literals inside the action body.
type GateVerdict = { ok: true; session: Session } | { ok: false; status: number; error: string };

// importGate merges the owner-only rule with the preview/commit rate budget.
// Owner-only: an import overwrites the board's commands/modules wholesale, and
// delegates are scoped to read-mostly sections by design. The route also sits
// outside every grantable section path, so the hooks guard already bounces
// delegates — this is defense in depth, same as settings/+page.server.ts.
// DEMO mints the fixture identity here because hooks never put a session in
// locals without OAuth; the demo board is an owner by construction.
async function importGate(locals: App.Locals): Promise<GateVerdict> {
  const s = await requireOwner(locals);
  if (!s) return { ok: false, status: 403, error: 'Not allowed.' };
  if (!(await importAllowed(s)))
    return { ok: false, status: 429, error: 'Too many import attempts. Wait a minute and try again.' };
  return { ok: true, session: s };
}

const SOURCES: readonly ImportSource[] = [
  'streamelements',
  'fossabot',
  'moobot',
  'nightbot',
  'streamlabs_desktop'
];

function isSource(v: string): v is ImportSource {
  return (SOURCES as readonly string[]).includes(v);
}

// requireOwner resolves the acting session (the DEMO fixture identity when
// DEMO=1) or null; importGate owns the policy reasoning above it.
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
  // Connected = the OAuth callback parked a token cookie that has not expired
  // or been consumed by a commit yet. DEMO reads connected so the wizard is
  // walkable without a real Nightbot app registration.
  return { nightbotConnected: DEMO || !!cookies.get(NB_TOKEN_COOKIE) };
};

// SourceInput carries the three form-borne inputs a preview may use: a pasted
// credential (StreamElements), an uploaded export (StreamLabs .db), or an
// already-parsed manifest (Moobot — the browser decoded its own export so the
// raw file never crosses the wire). Nightbot carries nothing on the form: its
// credential is the OAuth token cookie, resolved in the action itself.
interface SourceInput {
  credential: string;
  fileB64: string;
  preManifest?: ImportManifest;
}

// InputRefusal is one input-level rejection, or null when the source's inputs
// are acceptable.
type InputRefusal = { status: number; error: string } | null;

// JWT_SHAPE mirrors @bagel/shared/importer/streamelements: three
// dot-separated base64url segments. Failing here gives a readable message
// before any fetch is attempted and guarantees no credential with interior
// whitespace or control chars reaches the transport.
const JWT_SHAPE = /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/;

// SOURCE_INPUT_RULES decides per source whether the extracted inputs are
// acceptable; the action body stays guard → resolve → execute. The credential
// shape check runs for every source that kept one, exactly as before.
const SOURCE_INPUT_RULES: Record<Exclude<ImportSource, 'fossabot'>, (input: SourceInput) => InputRefusal> = {
  streamelements: (input) =>
    missingAnyInput(input, 'Paste your StreamElements JWT first.') ?? jwtShapeRefusal(input.credential),
  moobot: withJwtShapeCheck('Choose a file to upload.'),
  // Nightbot posts no form inputs at all: the OAuth connect flow parked the
  // access token in an HttpOnly cookie and nightbotCredential resolves it.
  nightbot: () => null,
  streamlabs_desktop: withJwtShapeCheck('Choose a file to upload.')
};

function withJwtShapeCheck(missingError: string): (input: SourceInput) => InputRefusal {
  return (input) => missingAnyInput(input, missingError) ?? jwtShapeRefusal(input.credential);
}

function missingAnyInput(input: SourceInput, error: string): InputRefusal {
  if (hasAnyInput(input)) return null;
  return { status: 400, error };
}

// hasAnyInput reports whether the post carried at least one of the three
// inputs a preview can consume.
function hasAnyInput(input: SourceInput): boolean {
  if (input.preManifest !== undefined) return true;
  return [input.credential, input.fileB64].some(nonEmpty);
}

function nonEmpty(v: string): boolean {
  return v !== '';
}

function jwtShapeRefusal(credential: string): InputRefusal {
  if (credential === '') return null;
  if (credential.length <= MAX_CREDENTIAL_LEN && JWT_SHAPE.test(credential)) return null;
  return {
    status: 400,
    error:
      'That does not look like a StreamElements JWT. Copy the whole token: three segments separated by dots, no spaces.'
  };
}

// decodePreManifest parses an optional posted manifest. It is untrusted input
// — it goes through validateManifest (caps, lengths, perms) in
// $lib/server/importer before anything renders or commits.
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

// readUpload base64-encodes an uploaded export, reporting whether one was
// present so the caller can clear any pasted credential (the two must never
// disagree about which to use).
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

// readSourceInput extracts the common form parts (manifest JSON first, then
// the upload, mirroring the original refusal order for doubly-bad posts) and
// applies the source's acceptance rule.
async function readSourceInput(
  form: FormData,
  source: Exclude<ImportSource, 'fossabot'>
): Promise<{ ok: true; input: SourceInput } | { ok: false; status: number; error: string }> {
  const pre = decodePreManifest(form);
  if (!pre.ok) return { ok: false, status: pre.status, error: pre.error };

  const up = await readUpload(form);
  if (!up.ok) return { ok: false, status: up.status, error: up.error };

  const input: SourceInput = {
    // Client-parsed path first: manifest present means no credential/file is
    // expected; a present upload clears any pasted credential.
    preManifest: pre.manifest,
    fileB64: up.fileB64,
    credential: up.uploaded ? '' : String(form.get('credential') ?? '').trim()
  };
  const refusal = SOURCE_INPUT_RULES[source](input);
  if (refusal) return { ok: false, ...refusal };
  return { ok: true, input };
}

// usableSource maps the posted source onto one preview can serve, or the
// refusal prose. Unknown strings and Fossabot (its parser is unregistered and
// its OAuth connect flow unbuilt since the importer folded into the dashboard;
// the card is disabled client-side, this rejects direct posts) collapse into
// one branch at the call site.
function usableSource(v: string): Exclude<ImportSource, 'fossabot'> | { error: string } {
  if (!isSource(v)) return { error: 'Pick a source to import from.' };
  if (v === 'fossabot') return { error: 'Fossabot import is not available yet.' };
  return v;
}

// resolveCredential picks the credential a preview fetches with. Nightbot's
// never rides the form: the OAuth callback parked the access token in an
// HttpOnly cookie and this is the only reader — null means the account is not
// connected (no cookie, or it expired) and the action refuses with the
// connect-first prose. Every other source uses whatever the form carried.
function resolveCredential(source: ImportSource, input: SourceInput, cookies: Cookies): string | null {
  if (source !== 'nightbot') return input.credential;
  const token = cookies.get(NB_TOKEN_COOKIE) ?? '';
  return token === '' ? null : token;
}

export const actions: Actions = {
// preview translates one source config into a reviewable manifest. The
// identity comes from the session; the form carries the source choice and one
// of the inputs described on SourceInput.
preview: async ({ request, locals, cookies }) => {
    const gate = await importGate(locals);
    if (!gate.ok) return fail(gate.status, { error: gate.error, step: 'preview' });

    const form = await request.formData();
    const source = usableSource(String(form.get('source') ?? ''));
    if (typeof source !== 'string') return fail(400, { error: source.error, step: 'preview' });

    const read = await readSourceInput(form, source);
    if (!read.ok) return fail(read.status, { error: read.error, step: 'preview' });

    if (DEMO) {
      const demo = await import('$lib/server/demo-import');
      return { ok: true, step: 'preview', source, preview: demo.demoImportPreview(source) };
    }

    const credential = resolveCredential(source, read.input, cookies);
    if (credential === null)
      return fail(400, { error: 'Connect your Nightbot account first.', step: 'preview' });

    let preview: PreviewResponse;
    try {
      preview = await previewImport(gate.session, {
        source,
        credential,
        file_b64: read.input.fileB64,
        manifest: read.input.preManifest
      });
    } catch {
      return fail(502, { error: 'The importer service did not answer. Try again in a moment.', step: 'preview' });
    }

    if (!preview.manifest)
      return fail(422, {
        error: preview.error || 'Nothing could be imported from that source.',
        step: 'preview'
      });

    // The manifest echoes back verbatim on commit (minus what the user
    // unchecks), so it must survive devalue serialization as plain data.
    return { ok: true, step: 'preview', source, preview };
  },

  // commit applies the reviewed manifest. The client filters unchecked items
  // out of the manifest JSON before submitting; the server trusts nothing
  // about who is asking beyond the session — $lib/server/importer re-runs
  // validateManifest (counts, lengths, perms, caps) over every incoming
  // manifest before writing, so a hand-edited POST cannot land junk.
  commit: async ({ request, locals, cookies }) => {
    const gate = await importGate(locals);
    if (!gate.ok) return fail(gate.status, { error: gate.error, step: 'commit' });

    const form = await request.formData();
    const source = String(form.get('source') ?? '');
    const overwrite = form.get('overwrite') === 'on';
    const rawManifest = String(form.get('manifest') ?? '');
    if (!rawManifest) return fail(400, { error: 'Nothing selected to import.', step: 'commit' });

    let manifest: ImportManifest;
    try {
      manifest = JSON.parse(rawManifest) as ImportManifest;
    } catch {
      return fail(400, { error: 'The selection could not be decoded. Run the preview again.', step: 'commit' });
    }

    if (DEMO) {
      const demo = await import('$lib/server/demo-import');
      return { ok: true, step: 'commit', commit: demo.demoImportCommit(manifest, overwrite) };
    }

    let commit: CommitResponse;
    try {
      commit = await commitImport(gate.session, {
        source: isSource(source) ? source : '',
        manifest,
        overwrite
      });
    } catch {
      return fail(502, { error: 'The importer service did not answer. Try again in a moment.', step: 'commit' });
    }

    if (commit.error)
      return fail(502, { error: commit.error, step: 'commit' });

    // The Nightbot token was needed for exactly one preview→commit round
    // trip; drop it as soon as the import lands rather than waiting out the
    // cookie's own 15-minute TTL.
    if (source === 'nightbot') cookies.delete(NB_TOKEN_COOKIE, { path: NB_COOKIE_PATH });

    return { ok: true, step: 'commit', commit };
  }
};
