// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { redirect } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';
import { markEdge } from '#lib/server/edge-cache';
import { readQueueChannel, requireQueueLogin } from '#lib/server/queue-channel';

function canonicalPath(url: URL, login: string): string {
  return `${url.pathname.slice(0, url.pathname.lastIndexOf('/') + 1)}${login}${url.search}`;
}

export const load: PageServerLoad = async ({ params, url, locals }) => {
  const segment = params.user ?? '';
  const login = requireQueueLogin(url, segment, locals);
  if (segment !== login) throw redirect(308, canonicalPath(url, login));

  const read = await readQueueChannel(login, locals);
  markEdge(locals, read.outcome);
  return { login: read.login, channelName: read.channelName, snapshot: read.snapshot };
};
