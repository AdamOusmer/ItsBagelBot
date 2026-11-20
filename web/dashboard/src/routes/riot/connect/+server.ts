// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { requireValorantActor, riotConfigured, riotFail, startRiotOAuth } from '#lib/server/riot-oauth';

export const GET: RequestHandler = ({ cookies, url, locals }) => {
  const actor = requireValorantActor(locals);
  if (!riotConfigured()) riotFail('unconfigured');
  throw redirect(302, startRiotOAuth(actor, cookies, url), { external: true });
};
