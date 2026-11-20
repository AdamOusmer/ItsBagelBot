// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { USERNAME_RE } from '@bagel/kit/twitch-names';

export { LOGIN_RE } from '@bagel/kit/twitch-names';

export const normalizeLogin = (raw: string): string => raw.replace(/^@+/, '').toLowerCase();

export const isLoginShape = (name: string): boolean => USERNAME_RE.test(name);
