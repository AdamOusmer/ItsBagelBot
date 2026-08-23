// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { dev } from '$app/environment';
import { fail, redirect } from '@sveltejs/kit';
import { previewImport, commitImport } from '$lib/server/importer';
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
const DEMO = dev && process.env.DEMO === '1';

const SOURCES: readonly ImportSource[] = ['streamelements', 'fossabot', 'moobot', 'streamlabs_desktop'];

function isSource(v: string): v is ImportSource {
  return (SOURCES as readonly string[]).includes(v);
}

// Owner-only: an import overwrites the board's commands/modules wholesale, and
// delegates are scoped to read-mostly sections by design. The route also sits
// outside every grantable section path, so the hooks guard already bounces
// delegates — this is defense in depth, same as settings/+page.server.ts.
// DEMO mints the fixture identity here because hooks never put a session in
// locals without OAuth; the demo board is an owner by construction.
async function requireOwner(locals: App.Locals): Promise<Session | null> {
  if (DEMO) {
    const { demoSession } = await import('$lib/server/demo-data');
    return demoSession();
  }
  const s = locals.session;
  if (!s || s.delegate_of) return null;
  return s;
}

export const load: PageServerLoad = async ({ locals }) => {
  const s = await requireOwner(locals);
  if (!s) throw redirect(302, '/');
  return {};
};

export const actions: Actions = {
// preview translates one source config into a reviewable manifest. The
// identity comes from the session; the form carries the source choice and one
// of: a pasted credential (StreamElements), an uploaded export (StreamLabs
// .db), or an already-parsed manifest (Moobot — the browser decoded its own
// export so the raw file never crosses the wire).
preview: async ({ request, locals }) => {
    const s = await requireOwner(locals);
    if (!s) return fail(403, { error: 'Not allowed.', step: 'preview' });
    if (!(await importAllowed(s)))
      return fail(429, { error: 'Too many import attempts. Wait a minute and try again.', step: 'preview' });

    const form = await request.formData();
    const source = String(form.get('source') ?? '');
    if (!isSource(source)) return fail(400, { error: 'Pick a source to import from.', step: 'preview' });

    // Fossabot needs an OAuth connect flow that does not exist yet (no parser
    // exists for it since the importer folded into the dashboard); the card is
    // disabled client-side, this rejects direct posts.
    if (source === 'fossabot')
      return fail(400, { error: 'Fossabot import is not available yet.', step: 'preview' });

    // Client-parsed path first: manifest present means no credential/file is
    // expected. The manifest is untrusted input — it goes through
    // validateManifest (caps, lengths, perms) in $lib/server/importer before
    // anything renders or commits.
    let preManifest: ImportManifest | undefined;
    const rawManifest = String(form.get('manifest') ?? '');
    if (rawManifest) {
      if (rawManifest.length > MAX_MANIFEST_JSON_BYTES)
        return fail(400, { error: 'The parsed import is too large to verify.', step: 'preview' });
      try {
        const parsed = JSON.parse(rawManifest) as ImportManifest;
        if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
          throw new Error('not an object');
        preManifest = parsed;
      } catch {
        return fail(400, { error: 'The parsed import could not be decoded. Run the preview again.', step: 'preview' });
      }
    }

    let credential = String(form.get('credential') ?? '').trim();
    let fileB64 = '';
    const upload = form.get('file');
    if (upload instanceof File && upload.size > 0) {
      if (upload.size > MAX_UPLOAD_BYTES)
        return fail(400, {
          error: 'That file is too large. Exported bot configs should be well under 20 MB.',
          step: 'preview'
        });
      try {
        fileB64 = Buffer.from(await upload.arrayBuffer()).toString('base64');
      } catch {
        return fail(400, { error: 'Could not read that file.', step: 'preview' });
      }
      credential = '';
    }

    if (!credential && !fileB64 && !preManifest)
      return fail(400, {
        error:
          source === 'streamelements'
            ? 'Paste your StreamElements JWT first.'
            : 'Choose a file to upload.',
        step: 'preview'
      });

    // Shape-check mirrors @bagel/shared/importer/streamelements: three
    // dot-separated base64url segments, <=4KB. Failing here gives a readable
    // message before any fetch is attempted, and guarantees no credential with
    // interior whitespace or control chars reaches the transport.
    if (
      credential &&
      (credential.length > 4096 ||
        !/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(credential))
    )
      return fail(400, {
        error:
          'That does not look like a StreamElements JWT. Copy the whole token: three segments separated by dots, no spaces.',
        step: 'preview'
      });

    if (DEMO) {
      const demo = await import('$lib/server/demo-import');
      return { ok: true, step: 'preview', source, preview: demo.demoImportPreview(source) };
    }

    let preview: PreviewResponse;
    try {
      preview = await previewImport(s, {
        source,
        credential,
        file_b64: fileB64,
        manifest: preManifest
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
  commit: async ({ request, locals }) => {
    const s = await requireOwner(locals);
    if (!s) return fail(403, { error: 'Not allowed.', step: 'commit' });
    if (!(await importAllowed(s)))
      return fail(429, { error: 'Too many import attempts. Wait a minute and try again.', step: 'commit' });

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
      commit = await commitImport(s, {
        source: isSource(source) ? source : '',
        manifest,
        overwrite
      });
    } catch {
      return fail(502, { error: 'The importer service did not answer. Try again in a moment.', step: 'commit' });
    }

    if (commit.error)
      return fail(502, { error: commit.error, step: 'commit' });

    return { ok: true, step: 'commit', commit };
  }
};
