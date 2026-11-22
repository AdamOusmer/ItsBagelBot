// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beginUpload, relayPart } from '#lib/server/overlay-upload';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = ({ locals, request }) => beginUpload('imports', locals.session, request);

export const PUT: RequestHandler = ({ locals, request, url }) => relayPart('imports', locals.session, request, url);
