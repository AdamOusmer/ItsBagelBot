// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { accountRoutes } from './wire/account';
import { integrationRoutes } from './wire/integrations';
import { loyaltyRoutes } from './wire/loyalty';
import { quoteRoutes } from './wire/quotes';
import type { Payload, Routes } from './wire/types';

const routes: Routes = { ...accountRoutes, ...loyaltyRoutes, ...integrationRoutes, ...quoteRoutes };

export function answer(subject: string, payload: unknown): unknown {
  const handler = routes[subject];
  return handler ? handler((payload ?? {}) as Payload) : { ok: true };
}
