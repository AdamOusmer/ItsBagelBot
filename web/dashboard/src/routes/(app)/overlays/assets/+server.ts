// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { deleteAsset, listAssets } from '#lib/server/overlay-control';
import { beginUpload, relayPart } from '#lib/server/overlay-upload';
import type { RequestHandler } from './$types';

export const GET: RequestHandler = ({ locals }) => listAssets(locals.session);

export const DELETE: RequestHandler = ({ locals, url }) => deleteAsset(locals.session, url);

export const POST: RequestHandler = ({ locals, request }) => beginUpload('assets', locals.session, request);

export const PUT: RequestHandler = ({ locals, request, url }) => relayPart('assets', locals.session, request, url);
