// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Fossabot has no import path yet: no parser is registered and no connect flow
// exists, so the client strategy marks it available: false, which ships the
// tile disabled and makes the form action refuse a direct post before this
// module is consulted at all.
//
// The entry exists so the registry stays a total map over ImportSource and no
// call site has to handle a missing strategy. Its acceptance rule states the
// same refusal the action gives, so the source is honest about itself even if
// a future caller reaches it directly; the real rule lands with the input.
import type { ServerSourceStrategy } from '../strategy';

export const fossabotSource: ServerSourceStrategy = {
  id: 'fossabot',
  acceptInput: () => ({ status: 400, error: 'Fossabot import is not available yet.' })
};
