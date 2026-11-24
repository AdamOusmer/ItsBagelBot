// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const MUTATING = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);
const FORM_TYPES = new Set([
  'application/x-www-form-urlencoded',
  'multipart/form-data',
  'text/plain',
  'application/x-sveltekit-formdata'
]);
const ONE_CLICK_PATH = /^\/u\/[^/]+\/?$/;

type Origined = { request: Request; url: URL };

function formLike(request: Request): boolean {
  const type = request.headers.get('content-type');
  if (!type) return true;
  return FORM_TYPES.has(type.split(';', 1)[0].trim().toLowerCase());
}

// SvelteKit's own check is off (csrf.trustedOrigins '*') because RFC 8058 mail clients POST /u/<token> with no Origin header.
export function crossSiteForm({ request, url }: Origined): boolean {
  if (!MUTATING.has(request.method) || !formLike(request)) return false;
  if (request.headers.get('origin') === url.origin) return false;
  return !(request.method === 'POST' && !url.search && ONE_CLICK_PATH.test(url.pathname));
}

export function sameOrigin({ request, url }: Origined): boolean {
  return request.headers.get('origin') === url.origin;
}
