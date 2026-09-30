// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { defineMiddleware } from 'astro:middleware';
import { getLangFromUrl } from './i18n/ui';

export const onRequest = defineMiddleware((context, next) => {
  context.locals.uiLocale = getLangFromUrl(context.url);
  return next();
});
