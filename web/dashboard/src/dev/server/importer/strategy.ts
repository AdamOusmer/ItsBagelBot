// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ImportSource } from '@bagel/kit';
import { SERVER_STRATEGIES as LIVE, type ServerSourceStrategy } from '#lib/server/importer/strategy';

export * from '#lib/server/importer/strategy';

function connectedDemo(strategy: ServerSourceStrategy): ServerSourceStrategy {
  return {
    ...strategy,
    connected: strategy.connected && (() => true),
    credential: strategy.credential && ((input) => input.credential)
  };
}

export const SERVER_STRATEGIES = Object.fromEntries(
  Object.entries(LIVE).map(([id, strategy]) => [id, connectedDemo(strategy)])
) as Record<ImportSource, ServerSourceStrategy>;
