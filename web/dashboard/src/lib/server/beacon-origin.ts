// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { SITE } from '@bagel/kit/site-links';

export function marketingFormForbidden(request: Request): boolean {
  if (!['POST', 'PUT', 'PATCH', 'DELETE'].includes(request.method)) return false;
  return request.headers.get('origin') === SITE.web;
}
