// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
// @ts-ignore Bun supplies this module at test runtime.
import { expect, test } from 'bun:test';
import { isPublicAdminRoute } from '../src/lib/server/public-routes';

test('bot OAuth can start, finish, and show its result without a staff session', () => {
  for (const path of ['/auth/bot/login', '/auth/bot/callback', '/auth/bot/done']) expect(isPublicAdminRoute(path)).toBe(true);
});

test('public bot authorization does not expose other admin routes', () => {
  for (const path of ['/', '/staff', '/secrets', '/auth/bot/token', '/auth/bot/login/other', '/auth/bot/callback-other']) expect(isPublicAdminRoute(path)).toBe(false);
});
