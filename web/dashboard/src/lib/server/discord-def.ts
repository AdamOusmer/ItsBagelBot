// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { moduleDef, type ModuleDef } from '@bagel/kit';

export const DISCORD_DEF: ModuleDef = (() => {
  const def = moduleDef('discord');
  if (!def) throw new Error('discord module missing from MODULE_CATALOG');
  return def;
})();
