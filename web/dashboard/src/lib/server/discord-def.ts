// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { moduleDef, type ModuleDef } from '@bagel/kit';

// Resolved once, and shared by both Discord routes rather than resolved in
// each. moduleDef returns undefined for an unknown id, and a silent undefined
// would disable the beta gate rather than fail, so this throws at import time
// if the catalog ever drops the entry.
export const DISCORD_DEF: ModuleDef = (() => {
  const def = moduleDef('discord');
  if (!def) throw new Error('discord module missing from MODULE_CATALOG');
  return def;
})();
