// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fetchNightbot, parseNightbot } from '@bagel/kit/importer/nightbot';
import { CODE } from '@bagel/kit/importer/validate';
import { NB_COOKIE_PATH, NB_TOKEN_COOKIE } from '$lib/server/nightbot-oauth';
import type { Cookies } from '@sveltejs/kit';
import { refused, type ImportPreviewRequest, type ParseOutcome } from '../engine';
import type { ServerSourceStrategy } from '../strategy';

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

function credential(_input: unknown, cookies: Cookies): string | null {
  const token = cookies.get(NB_TOKEN_COOKIE) ?? '';
  return token === '' ? null : token;
}

export const nightbotSource: ServerSourceStrategy = {
  id: 'nightbot',
  acceptInput: () => null,
  credential,
  connected: (cookies) => !!cookies.get(NB_TOKEN_COOKIE),
  afterCommit: (cookies) => cookies.delete(NB_TOKEN_COOKIE, { path: NB_COOKIE_PATH }),
  leg: nightbotLeg
};
