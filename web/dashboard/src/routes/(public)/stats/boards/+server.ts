// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { publicBoards } from '$lib/server/public-boards';

export const GET: RequestHandler = async () =>
  json(await publicBoards(), { headers: { 'cache-control': 'no-store' } });
