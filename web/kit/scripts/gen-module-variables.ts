// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { MODULE_VARIABLE_SPECS } from '../lib/variables/module-variables';

// Regenerate the Go runtime inventory after changing a module's reply palette.
await Bun.write(new URL('../../../internal/domain/modulevars/catalog.json', import.meta.url),
  JSON.stringify(MODULE_VARIABLE_SPECS.map(({ id, groups }) => ({ id,
    groups: groups.map(({ name, fields, messageKey }) => ({ name, fields, message_key: messageKey }))
  })), null, 2) + '\n');
