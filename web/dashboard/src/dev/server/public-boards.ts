// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PublicBoards } from '#lib/server/public-boards';
import { demoBoards } from '../fixtures';

export * from '#lib/server/public-boards';

export async function publicBoards(): Promise<PublicBoards> {
  return demoBoards();
}
