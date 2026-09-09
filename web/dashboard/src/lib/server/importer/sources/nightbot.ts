// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Nightbot: the only connect-first source. Its credential never rides the
// form. The OAuth callback route parks the access token in an HttpOnly cookie
// and the hooks below are the only readers, so a hand-made post cannot smuggle
// a token in and the page never sees one.
import { fetchNightbot, parseNightbot } from '@bagel/kit/importer/nightbot';
import { CODE } from '@bagel/kit/importer/validate';
import { NB_COOKIE_PATH, NB_TOKEN_COOKIE } from '$lib/server/nightbot-oauth';
import type { Cookies } from '@sveltejs/kit';
import { refused, type ImportPreviewRequest, type ParseOutcome } from '../engine';
import type { ServerSourceStrategy } from '../strategy';

// nightbotLeg pulls the account's config over the Nightbot REST API with the
// OAuth access token (req.credential: resolved from the cookie by credential()
// below, never off user paste) and feeds the stapled envelope through the
// shared parser.
async function nightbotLeg(req: ImportPreviewRequest): Promise<ParseOutcome> {
  let envelope: Uint8Array;
  try {
    const fetched = await fetchNightbot(req.credential ?? '');
    envelope = new TextEncoder().encode(JSON.stringify(fetched));
  } catch (err) {
    return refused({ code: CODE.fetchFailed, message: (err as Error).message });
  }
  try {
    const parsed = parseNightbot(envelope);
    return { manifest: parsed.manifest, diags: [...parsed.diagnostics] };
  } catch (err) {
    return refused({ code: CODE.parseFailed, message: (err as Error).message });
  }
}

// credential returns null when the account is not connected (no cookie, or it
// expired), which the action turns into the connect-first refusal.
function credential(_input: unknown, cookies: Cookies): string | null {
  const token = cookies.get(NB_TOKEN_COOKIE) ?? '';
  return token === '' ? null : token;
}

export const nightbotSource: ServerSourceStrategy = {
  id: 'nightbot',
  // Nightbot posts no form inputs at all, so there is nothing to refuse here.
  acceptInput: () => null,
  credential,
  connected: (cookies) => !!cookies.get(NB_TOKEN_COOKIE),
  // The token was needed for exactly one preview → commit round trip; drop it
  // as soon as the import lands rather than waiting out the cookie's own
  // 15-minute TTL.
  afterCommit: (cookies) => cookies.delete(NB_TOKEN_COOKIE, { path: NB_COOKIE_PATH }),
  leg: nightbotLeg
};
