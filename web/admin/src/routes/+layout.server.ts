// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { LayoutServerLoad } from './$types';

// Root layout data is inherited by every route (including the pre-login
// /login page), so the resolved locale reaches the i18n context no matter
// where the operator lands.
export const load: LayoutServerLoad = ({ locals }) => ({
  locale: locals.locale
});
