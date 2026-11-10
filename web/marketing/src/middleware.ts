// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { defineMiddleware } from 'astro:middleware';
import { getLangFromUrl } from './i18n/ui';

const MODULE_SCRIPT = '<script type="module"';
// Rocket Loader rewrites script types, so pages swapped in by the router never run theirs. The attribute must precede src.
const EDGE_EXEMPT_MODULE_SCRIPT = '<script data-cfasync="false" type="module"';

export const onRequest = defineMiddleware(async (context, next) => {
  context.locals.uiLocale = getLangFromUrl(context.url);
  const response = await next();
  if (!response.headers.get('content-type')?.includes('text/html')) return response;
  const html = await response.text();
  return new Response(html.replaceAll(MODULE_SCRIPT, EDGE_EXEMPT_MODULE_SCRIPT), response);
});
