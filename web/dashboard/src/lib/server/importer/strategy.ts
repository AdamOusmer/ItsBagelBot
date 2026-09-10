// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Server half of the import-source registry: what a source needs that only the
// server may do (read an HttpOnly cookie, fetch an upstream API, decode an
// upload). The client half (@bagel/kit/importer/strategy) carries what the
// page renders.
//
// This replaces the per-source tables the form action used to carry
// (SOURCE_INPUT_RULES, resolveCredential, the Nightbot cookie special cases in
// load and commit). The action now looks a source up once and calls the hooks
// it declares, so it names no source at all.
import type { Cookies } from '@sveltejs/kit';
import type { ImportManifest, ImportSource } from '@bagel/kit';
import type { ImportPreviewRequest, ParseOutcome } from './engine';
import { streamelementsSource } from './sources/streamelements';
import { fossabotSource } from './sources/fossabot';
import { moobotSource } from './sources/moobot';
import { nightbotSource } from './sources/nightbot';
import { streamlabsDesktopSource } from './sources/streamlabs-desktop';
import { wizebotSource } from './sources/wizebot';

// SourceInput carries the three form-borne inputs a preview may use: a pasted
// credential (StreamElements), an uploaded export (StreamLabs .db), or an
// already-parsed manifest (Moobot, the browser decoded its own export so the
// raw file never crosses the wire). Nightbot carries nothing on the form: its
// credential is the OAuth token cookie, read by its own credential() hook.
export interface SourceInput {
  credential: string;
  fileB64: string;
  preManifest?: ImportManifest;
}

// InputRefusal is one input-level rejection, or null when the source's inputs
// are acceptable.
export type InputRefusal = { status: number; error: string } | null;

// ServerSourceStrategy is one source's server-side behaviour. Only acceptInput
// is mandatory: the optional hooks say what this source additionally has (an
// OAuth token cookie, a fetch/parse leg, a cookie to burn after a commit), and
// their absence is what keeps the action free of per-source branches.
export interface ServerSourceStrategy {
  id: ImportSource;
  // acceptInput decides whether the extracted form inputs are usable, so the
  // action body stays guard → resolve → execute.
  acceptInput(input: SourceInput): InputRefusal;
  // credential resolves what the preview fetches with; null means "not
  // connected" and the action refuses with the connect-first prose. Absent
  // means the form's own credential field is used as-is.
  credential?(input: SourceInput, cookies: Cookies): string | null;
  // connected feeds load() → page.data.connected[id]. Only a source with a
  // connect step declares it.
  connected?(cookies: Cookies): boolean;
  // leg fetches and parses. Absent for a browser-parsed source, which posts
  // its manifest and skips the engine's fetch/parse entirely.
  leg?(req: ImportPreviewRequest): Promise<ParseOutcome>;
  // afterCommit runs once an import has landed (Nightbot burns its token
  // cookie there rather than waiting out the TTL).
  afterCommit?(cookies: Cookies): void;
}

export const SERVER_STRATEGIES: Record<ImportSource, ServerSourceStrategy> = {
  streamelements: streamelementsSource,
  fossabot: fossabotSource,
  moobot: moobotSource,
  nightbot: nightbotSource,
  streamlabs_desktop: streamlabsDesktopSource,
  wizebot: wizebotSource
};

// missingAnyInput refuses a post that carried none of the inputs a preview can
// consume, with the prose that names what THIS source was waiting for.
export function missingAnyInput(input: SourceInput, error: string): InputRefusal {
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

// fileSourceInput is the acceptance rule both file-backed sources share: a
// file (or the manifest the browser parsed out of one) must be present, and
// any credential riding along is still shape-checked exactly as it was before
// the registry split.
export function fileSourceInput(input: SourceInput): InputRefusal {
  return missingAnyInput(input, 'Choose a file to upload.') ?? credentialShapeRefusal(input.credential);
}

// MAX_CREDENTIAL_LEN and JWT_SHAPE mirror @bagel/kit/importer/streamelements:
// three dot-separated base64url segments, <=4KB. Failing here gives a readable
// message before any fetch is attempted and guarantees no credential with
// interior whitespace or control chars reaches the transport. It runs for
// every source that kept a credential field, which is how it has always
// behaved: a file source posting a malformed one is a hand-made request.
const MAX_CREDENTIAL_LEN = 4096;
const JWT_SHAPE = /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/;

export function credentialShapeRefusal(credential: string): InputRefusal {
  if (credential === '') return null;
  if (credential.length <= MAX_CREDENTIAL_LEN && JWT_SHAPE.test(credential)) return null;
  return {
    status: 400,
    error:
      'That does not look like a StreamElements JWT. Copy the whole token: three segments separated by dots, no spaces.'
  };
}
