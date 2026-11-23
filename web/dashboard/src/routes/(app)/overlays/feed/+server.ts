// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { controlFeed } from '#lib/server/overlay-control';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = ({ locals, request }) => controlFeed(locals.session, request);
