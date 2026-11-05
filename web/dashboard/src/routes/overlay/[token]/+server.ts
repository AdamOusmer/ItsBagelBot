// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { overlayNotFound, overlayPageResponse, overlayUnavailable } from '#lib/server/overlay-page';
import { moduleTokenStore, verifyOverlayToken } from '#lib/server/overlay-token';
import type { RequestHandler } from './$types';

export const GET: RequestHandler = async ({ params }) => {
  try {
    const verified = await verifyOverlayToken(params.token, moduleTokenStore);
    return verified ? overlayPageResponse() : overlayNotFound();
  } catch {
    return overlayUnavailable();
  }
};
