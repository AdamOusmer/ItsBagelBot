// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { overlayHub, overlayStreamResponse } from '#lib/server/overlay-hub';
import { overlayNotFound, overlayUnavailable } from '#lib/server/overlay-page';
import { moduleTokenStore, verifyOverlayToken } from '#lib/server/overlay-token';
import type { RequestHandler } from './$types';

export const GET: RequestHandler = async ({ params, request }) => {
  try {
    const verified = await verifyOverlayToken(params.token, moduleTokenStore);
    if (!verified) return overlayNotFound();
    return overlayStreamResponse(request, overlayHub, verified.channelId, verified.tokenHash);
  } catch {
    return overlayUnavailable();
  }
};
