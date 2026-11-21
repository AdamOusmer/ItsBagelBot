// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { LayoutServerLoad } from './$types';
import { CURSOR_COOKIE } from '#lib/server/preference-cookies';

export const load: LayoutServerLoad = ({ cookies, locals }) => ({
  locale: locals.locale,
  cursorEnabled: cookies.get(CURSOR_COOKIE) !== '0'
});
