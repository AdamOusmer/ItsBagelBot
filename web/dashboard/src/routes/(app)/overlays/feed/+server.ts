// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { alphaOnly } from '#lib/server/alpha';
import { controlFeed } from '#lib/server/overlay-control';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = alphaOnly(({ locals, request }) => controlFeed(locals.session, request));
