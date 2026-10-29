// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fetchNightbot, parseNightbot } from '@bagel/kit/importer/nightbot';
import { NB_COOKIE_PATH, NB_TOKEN_COOKIE } from '#lib/server/nightbot-oauth';
import type { Cookies } from '@sveltejs/kit';
import { fetchAndParse, type ImportPreviewRequest, type ParseOutcome } from '../outcome';
import type { ServerSourceStrategy } from '../strategy';

function nightbotLeg(req: ImportPreviewRequest): Promise<ParseOutcome> {
  return fetchAndParse(
    async () => new TextEncoder().encode(JSON.stringify(await fetchNightbot(req.credential ?? ''))),
    parseNightbot
  );
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
